package myTCP

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"log"
	"math"
	"math/rand"
	"net"
	"time"
)

const (
	//States for Client
	HS1         = iota //0
	HS2         = iota //1
	Established = iota //2
	FinWait1    = iota //3
	FinWait2    = iota //4
	TimeWait    = iota //5
)

/* Client Side Functionality */

// clientConnection contains all state information for a reliable connection.
type clientConnection struct {
	UdpConn         *net.UDPConn
	sAddr           *net.UDPAddr
	state           uint16
	app_chan        chan string
	read_chan       chan []byte
	rcv_seg_chan    chan TCP_Segment
	send_seg_chan   chan TCP_Segment
	finished_chan   chan int
	clientPort      uint16
	serverPort      uint16
	clientSeq       uint32
	serverSeq       uint32
	last_seg_sent   TCP_Segment
	response        []byte
	server_rcvr_win uint16
}

/*     TCPs reliable data delivery service		*/
//Like GBN: 
//			Use cumulative ACKS.
//			Sender only remembers smallest seq# of outstanding segments(send_base) 
//			    and the seq# of the next byte to be sent(next_send).
//UNlike GBN: 
//			TCP ONLY retransmits the segment that caused the timeout.

//Like SR: 
//			Out-of-order segments are buffered.
//UNlike SR: 
//			Out-of-order segments are NOT individually ACKed. Receiver ONLY
//			    sends cumulative ACKs for the last correctly received in-order segment.
//
// Also there is Fast Retransmission => Duplicate ACKs signal a lost packet.

func Dial(addr string) clientConnection {
	var conn clientConnection
	//initialize the FSMs state
	conn.state = HS1
	//initialize channels
	conn.app_chan = make(chan string)
	conn.read_chan = make(chan []byte)
	conn.finished_chan = make(chan int)
	conn.send_seg_chan = make(chan TCP_Segment)
	conn.rcv_seg_chan = make(chan TCP_Segment)

	go conn.HandleConnection()
	conn.app_chan <- addr // SEND APP //

	return conn
}

func (conn *clientConnection) Write(data string) { conn.app_chan <- data }
func (conn *clientConnection) Close()            { conn.app_chan <- "Close Connection" }
func (conn *clientConnection) Read() []byte      { return <-conn.read_chan }

// HandleConnection() runs in background of HTTP(APP - LAYER)
// Called in Dial() - Receives server address from Dial
// Sends segments to server for rdt
// Receives segments from server and hands them off to rdt
// Handles demultiplexing
func (conn *clientConnection) HandleConnection() {
	//Handle connection must:
	//		Set timer on send.
	//		Alert rdt if timeout occurs.        conn.timeout <- "timeout"
	var err error

	///////////////// Handle 'Closed' state /////////////////	
	addr := <-conn.app_chan
	conn.sAddr, err = net.ResolveUDPAddr("udp4", addr)
	conn.UdpConn, err = net.DialUDP("udp4", nil, conn.sAddr)
	checkError(err)

	conn.clientPort = uint16(conn.UdpConn.LocalAddr().(*net.UDPAddr).Port)
	conn.serverPort = uint16(conn.sAddr.Port)
	rand.Seed(time.Now().UTC().UnixNano())
	conn.clientSeq = uint32(rand.Intn(65535))
	///////////////// Handle 'Closed' state /////////////////

	// Client FSM that runs in background
	go conn.rdt()

	// SEND and RECEIVE with goroutine closures //
	// go func() {
	//		 ... 
	//	}()
	///////////////// Send Packets /////////////////
	go func(conn *clientConnection) {
		var seg TCP_Segment
		var dataEmpty [MSS]byte
		for {
			seg = <-conn.send_seg_chan // RCV from SEGSEND //
			if seg.Header.Flags != ACK {
				conn.UdpConn.SetReadDeadline(time.Time{}) //stop timer
				log.Println("->TIMER Reset")
			}
			if segmentDebug {
				if seg.Data != dataEmpty {
					log.Println("->Sent Segment: ", seg)
				} else {
					log.Println("->Sent Segment: ", seg.Header)
				}
			}
			if seg.Header.Flags != ACK {
				conn.UdpConn.SetReadDeadline(time.Now().Add(3 * time.Second)) //set timer
			}

			conn.last_seg_sent = seg
			packet := seg.Serialize()

			myNetwork_clientSend(conn.UdpConn, packet)
		}
	}(conn)
	///////////////// Send Packets /////////////////
	///////////////// Receive Packets ///////////////// 
	go func(conn *clientConnection) {
		var buffer [UDP_BUF_LEN]byte
		var sAddr *net.UDPAddr
		var n int
		var segRcvd TCP_Segment
		var err error
		var servPort uint16
		for {
			log.Println("<-Waiting for a segment")
			n, sAddr, err = conn.UdpConn.ReadFromUDP(buffer[0:])
			if err != nil { //timeout occured
				log.Println("<-TIMEOUT OCCURED FOR SEGMENT: ", conn.last_seg_sent.Header)
				conn.send_seg_chan <- conn.last_seg_sent // SEND to SEGSEND //
			} else {
				servPort = uint16(sAddr.Port)
				slice := buffer[0:n]
				b := bytes.NewBuffer(slice)
				binary.Read(b, binary.BigEndian, &segRcvd)
				if segmentDebug {
					log.Println("<-Received Segment: ", segRcvd.Header)
					log.Println("<-Expecting seq num: ", conn.clientSeq)
				}

				if segRcvd.Header.DstPort == conn.clientPort && servPort == conn.serverPort { //Demultiplex
					log.Println("<-Stopped Timer for segment: ", segRcvd.Header)
					conn.UdpConn.SetReadDeadline(time.Time{}) //stop timer
					conn.rcv_seg_chan <- segRcvd              // SEND to SEGRCV //
				}
			}
		}
	}(conn)
	///////////////// Receive Packets /////////////////

	<-conn.finished_chan //Allows goroutine to finish
	log.Println("Finished")
}

func (conn *clientConnection) rdt() {

	var dataEmpty [MSS]byte //used for empty messages
	//var emptySegment TCP_Segment //used to reset segments
	var send_seg TCP_Segment
	var synack_seg TCP_Segment
	var ack_seg TCP_Segment
	var fin_seg TCP_Segment
	var msg_len int
	var seg_rcvd TCP_Segment

	/// CLIENT/
	for {
		switch conn.state {
		// First 2 cases handle 3 way Hand Shake
		case HS1:
			log.Println("HS1")
			// Part 1 of handshake
			//first seg -> no data, SYN bit set, randomly choosen clientSequence
			send_seg = TCP_Segment{Header: TCP_Header{SrcPort: conn.clientPort, DstPort: conn.serverPort,
				Seq: conn.clientSeq, Ack: 0, Flags: SYN, WindowSize: WIN_SIZE},
				Data: dataEmpty}
			// A SYN segment consumes one seq number			
			conn.clientSeq = conn.clientSeq + 1

			conn.send_seg_chan <- send_seg // SEND to SEGSEND //

			conn.state = HS2
		case HS2:
			log.Println("HS2")
			var correct_synack bool
			server_ok := false
			for {
				synack_seg = <-conn.rcv_seg_chan // RCV from SEGRCV //
				ack_seg, correct_synack = conn.checkSynAck(synack_seg)
				if correct_synack {
					conn.send_seg_chan <- ack_seg // SEND to SEGSEND //
					time.Sleep(6 * time.Second)   //give server a second to retransmit
					select {
					case synack_seg = <-conn.rcv_seg_chan: //server timed out and resent synack			// RCV from SEGRCV //
						log.Println("server timed out and resent synack")
						break //continue the for loop
					default: // server correctly received the ack
						log.Println("server correctly received the ack")
						server_ok = true
						break
					}
				}

				if server_ok { //HS3 complete
					break
				}
			}

			conn.state = Established
		case Established:
			// conn.Write(data string) will send a message along to the channel to send out
			//	{conn.channel <- data}
			// OR
			// conn.Close() will send a message to close the channel
			//	{conn.channel <- "Close Connection"}
			data := <-conn.app_chan

			if data == "Close Connection" {
				conn.state = FinWait1
			} else { //Write
				segmentList := conn.segmentMsg(data) //break message up into segments
				log.Println("Established -> Write")
				for e := segmentList.Front(); e != nil; e = e.Next() {
					send_seg = e.Value.(TCP_Segment)

					//increment client sequence in order to ack the correct segment
					msg_len = segment_data_len(send_seg.Data)
					conn.clientSeq = conn.clientSeq + uint32(msg_len)

					for {
						conn.send_seg_chan <- send_seg // SEND to SEGSEND //
						log.Println("Sent segment, waiting for ack")
						ack_seg = <-conn.rcv_seg_chan // RCV from SEGRCV //
						//check that flags are ACK
						if ack_seg.Header.Flags == ACK {
							//check that acknowledgement is correct
							if ack_seg.Header.Ack == conn.clientSeq {
								// update server buffer window size
								log.Println("Break the for loop")
								break
							}
						}
					}
				}

				// Now read response from the server	
				log.Println("--Made it to response from server--")
				server_done := false
				for {
					seg_rcvd = <-conn.rcv_seg_chan

					// Update server sequence and ACK the segment
					msg_len := segment_data_len(seg_rcvd.Data)

					conn.serverSeq = conn.serverSeq + uint32(msg_len)
					ack_seg = TCP_Segment{
						Header: TCP_Header{SrcPort: conn.clientPort, DstPort: conn.serverPort, Seq: conn.clientSeq,
							Ack: conn.serverSeq, Flags: ACK, WindowSize: WIN_SIZE},
						Data: dataEmpty}

					conn.send_seg_chan <- ack_seg

					//<-time.After(3 * time.Second) //wait 3 seconds in case the server didnt receive the ack
					select {
					case seg_rcvd = <-conn.rcv_seg_chan:
						log.Println("Continue")

						continue
					default: //if channel is empty, then server received the last ack
						log.Println("Break")
						server_done = true
						break
					}
					if server_done {
						break
					}
				}

				log.Println("--Sending file to app--")
				conn.read_chan <- seg_rcvd.Data[:]

			}

		case FinWait1:
			// Send FIN then Receive ACK and Send nothing
			// ACK alone does not consume Server Seq num
			log.Println("FINWAIT1")

			// FIN segment consumes one seq num if it carries no data
			conn.clientSeq = conn.clientSeq + 1

			send_seg = TCP_Segment{Header: TCP_Header{SrcPort: conn.clientPort, DstPort: conn.serverPort,
				Seq: conn.clientSeq, Ack: conn.serverSeq, Flags: FIN, WindowSize: WIN_SIZE},
				Data: dataEmpty}

			for {
				log.Println("Sending FIN")
				conn.send_seg_chan <- send_seg

				ack_seg = <-conn.rcv_seg_chan
				//check that flags are ACK
				if ack_seg.Header.Flags == ACK {
					//check that acknoledgement is correct
					if ack_seg.Header.Ack == conn.clientSeq {
						break
					}
				}
			}

			conn.state = FinWait2
		case FinWait2:
			log.Println("FINWAIT2")
			// Receive FIN and Send ACK
			// ACK with no data does not consume a seq num

			// After sending ack, must wait in case ack drops, and server sends fin again

			fin_seg = <-conn.rcv_seg_chan
			server_done := false
			for {
				//fin_seg = <-conn.rcv_seg_chan
				//log.Println("Fin Seg: ", fin_seg)

				//check that flags are FIN from server
				if fin_seg.Header.Flags == FIN {
					//check that acknoledgement is correct
					if fin_seg.Header.Ack == conn.clientSeq {
						// FIN segment consumes one seq num if it carries no data
						//conn.serverSeq = conn.serverSeq + 1

						//send ack
						ackSeg := TCP_Segment{Header: TCP_Header{SrcPort: conn.clientPort, DstPort: conn.serverPort,
							Seq: conn.clientSeq, Ack: conn.serverSeq + 1, Flags: ACK, WindowSize: WIN_SIZE},
							Data: dataEmpty}
						// An ACK segment with no data consumes no seq num
						conn.send_seg_chan <- ackSeg

						//<-time.After(10 * time.Second) //wait 10 seconds						
						select {
						case fin_seg = <-conn.rcv_seg_chan:
							log.Println("Continue")
							break
						default: //if channel is empty, then server received the last ack
							log.Println("Break")
							server_done = true
							break
						}
					}
				}

				if server_done {
					break
				}
			}
			conn.state = TimeWait
		case TimeWait:
			// Wait 30 seconds then close
			conn.finished_chan <- 1
			break
		}
	}

}

func (conn *clientConnection) segmentMsg(data string) *list.List {
	msgBytes := []byte(data)
	l := len(data)
	numSegments := int(math.Ceil(float64(l) / float64(MSS)))
	remainder := l % MSS

	segmentList := list.New()

	// segment(split up) the message and create TCP_Segment(s) for it
	var segment TCP_Segment
	var emptySegment TCP_Segment

	// DONT ASSIGN SEQUENCE NUMBER HERE, just use a local variable
	client_sequence := conn.clientSeq

	if numSegments == 1 {
		segment = emptySegment
		segment.Header.SrcPort = conn.clientPort
		segment.Header.DstPort = conn.serverPort

		segment.Header.Seq = conn.clientSeq
		segment.Header.Ack = conn.serverSeq
		segment.Header.Flags = NONE
		segment.Header.WindowSize = WIN_SIZE

		copy(segment.Data[0:l], msgBytes[0:l])
		//conn.clientSeq = conn.clientSeq + uint32(l)
		client_sequence = client_sequence + uint32(l)
		segmentList.PushBack(segment)
	} else {
		for i := 0; i < int(numSegments); i++ {
			segment = emptySegment
			segment.Header.SrcPort = conn.clientPort
			segment.Header.DstPort = conn.serverPort

			segment.Header.Seq = conn.clientSeq
			segment.Header.Ack = conn.serverSeq
			segment.Header.Flags = NONE
			segment.Header.WindowSize = WIN_SIZE

			if i != int(numSegments)-1 {
				copy(segment.Data[0:MSS], msgBytes[i*MSS:i*MSS+MSS])
				//Segment contains MSS bytes => We need to increment clientSeq by MSS
				//conn.clientSeq = conn.clientSeq + uint32(MSS)
				client_sequence = client_sequence + uint32(MSS)
				segmentList.PushBack(segment)

			} else {
				//log.Println(msgBytes[i*MSS : i*MSS+remainder])
				copy(segment.Data[0:MSS], msgBytes[i*MSS:i*MSS+remainder])
				//Segment contains remainder bytes => We need to increment clientSeq by remainder
				//conn.clientSeq = conn.clientSeq + uint32(remainder)
				client_sequence = client_sequence + uint32(remainder)
				segmentList.PushBack(segment)

			}
		}
	}

	return segmentList
}

func responseValid(seg TCP_Segment) bool {
	var emptyData [MSS]byte
	if seg.Data != emptyData {
		return true
	}
	return false
}

func (conn *clientConnection) checkSynAck(synack_seg TCP_Segment) (TCP_Segment, bool) {
	var ack_seg TCP_Segment
	var dataEmpty [MSS]byte
	//check that flags are SYNACK
	if synack_seg.Header.Flags == SYNACK {
		//check that acknoledgement is correct
		if synack_seg.Header.Ack == conn.clientSeq {
			//store server initial sequence
			conn.serverSeq = synack_seg.Header.Seq + 1        //<-- server spent one byte for the syn flag
			ack_seg = TCP_Segment{Header: TCP_Header{SrcPort: conn.clientPort, DstPort: conn.serverPort,
				Seq: conn.clientSeq, Ack: conn.serverSeq, Flags: ACK, WindowSize: WIN_SIZE},
				Data: dataEmpty}
			// An ACK segment with no data consumes no seq num
			return ack_seg, true
		}

	}
	var emptySeg TCP_Segment
	return emptySeg, false
}

func myNetwork_clientSend(c *net.UDPConn, packet []byte) {
	if rand.Intn(4) != 10 { //25% failure rate
		c.Write(packet)
	} else {
		log.Println("Dropped Packet: ", packet)
	}

}
