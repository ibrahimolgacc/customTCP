package myTCP

import (
	"bytes"
	"encoding/binary"

	"log"
	"os"
)

const (
	MSS         int    = 1024 // Maximum Segment Size
	W_SEGS      int    = 1    // Receive Buffer Window Size
	WIN_SIZE    uint16 = 1024
	UDP_BUF_LEN        = WIN_SIZE * 2 // UDP must be able to read > [WIN_SIZE]byte
	SEGMENT_LEN int    = 271          //Had to hard-code, buffers will store segments, not their payloads

)

const (
	packetDebug  bool = false
	segmentDebug bool = true
	messageDebug bool = false
)

type TCP_Segment struct {
	Header TCP_Header
	Data   [MSS]byte
}

type TCP_Header struct {
	SrcPort    uint16
	DstPort    uint16
	Seq        uint32
	Ack        uint32
	Flags      uint8
	WindowSize uint16
}

//Enumerated Types
const (
	//Flags <= Must be 8 bit
	NONE   = 1 << iota // 1
	SYN    = 1 << iota // 2
	SYNACK = 1 << iota // 4
	ACK    = 1 << iota // 8
	FIN    = 1 << iota // 16
	//FINACK = 1 << iota // 32
)

func segment_data_len(data [MSS]byte) int {
	len := 0
	for i := range data {
		if data[i] != 0 {
			len = len + 1
		}
	}
	return len
}

/* General Functionality */
// Serialize returns a byte slice that represents the binary encoded TCP_Segment.
func (seg *TCP_Segment) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, seg)
	return buf.Bytes()
}

// Deserialize returns the TCP_SEGMENT encoded in a receive [RECV_BUF_LEN]buffer
func Deserialize(packet [UDP_BUF_LEN]byte, n int) TCP_Segment {
	temp := packet[0:n]
	b := bytes.NewBuffer(temp)
	//log.Println("Buffer:", b.Bytes())
	var segmentReceived TCP_Segment
	binary.Read(b, binary.BigEndian, &segmentReceived)
	//log.Println("Segment Rec: ", segmentReceived)
	return segmentReceived
}

func checkError(err error) {
	if err != nil {
		log.Println(os.Stderr, "Fatal error:%s", err.Error())
		os.Exit(1)
	}
}
