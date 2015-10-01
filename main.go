// Copyright 2015 Florian Hülsmann <fh@cbix.de>

package main

import (
	"github.com/mediocregopher/radix.v2/redis"
	//"github.com/mediocregopher/radix.v2/pubsub"
	"fmt"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
	"net"
)

const (
	ProtocolIPv6ICMP = 58
)

var (
	pc *net.IPConn
	db *redis.Client
)

func main() {
	// open redis connection
	redisdb, err := redis.Dial("tcp", "localhost:6379")
	if err != nil {
		panic(err)
	}
	db = redisdb
	defer db.Close()

	// open listening connection
    conn, err := net.ListenIP("ip6:ipv6-icmp", &net.IPAddr{net.IPv6unspecified, ""})
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	pc = conn
	ipconn := ipv6.NewPacketConn(conn)

	filter := new(ipv6.ICMPFilter)
	filter.SetAll(true)
	filter.Accept(ipv6.ICMPTypeRouterSolicitation)
	//filter.Accept(ipv6.ICMPTypeRouterAdvertisement)
	//filter.Accept(ipv6.ICMPTypeNeighborSolicitation)
	//filter.Accept(ipv6.ICMPTypeNeighborAdvertisement)
	//filter.Accept(ipv6.ICMPTypeRedirect)
	if err := ipconn.SetICMPFilter(filter); err != nil {
		panic(err)
	}

	// read from socket
	err = nil
	buf := make([]byte, 512)
	var srcAddr net.Addr
	//var body []byte
	var n int
	for err == nil {
		if n, _, srcAddr, err = ipconn.ReadFrom(buf); err != nil {
			fmt.Println(err)
			continue
		}

		go handleND(srcAddr, buf[:n])
	}
	fmt.Printf("error: %v\n", err)
}

func handleND(src net.Addr, body []byte) {
	t := ipv6.ICMPType(body[0])
	fmt.Printf("message from %v type: %v\n", src, t)
	switch t {
	case ipv6.ICMPTypeRouterSolicitation:
		handleRS(src, body)
		/*
        			case ipv6.ICMPTypeRouterAdvertisement:
				handleRA(src, body)
			case ipv6.ICMPTypeNeighborSolicitation:
				handleNS(src, body)
			case ipv6.ICMPTypeNeighborAdvertisement:
				handleNA(src, body)
			case ipv6.ICMPTypeRedirect:
				handleRedirect(src, body)
		*/
	default:
		return
	}
}

func handleRS(src net.Addr, body []byte) {
	options, err := parseOptions(body[8:])
	if err != nil {
		fmt.Println(err)
	}
	// look up lla
	var lla *NDOptionLLA = nil
	for _, o := range options {
		if o == nil {
			continue
		}
		llaopt, ok := (*o).(*NDOptionLLA)
		if !ok {
			continue
		}
		lla = llaopt
		if int(lla.OptionType) != 1 {
			continue
		}
	}
	if lla == nil {
		return
	}
	fmt.Println("looking up prefix for " + net.HardwareAddr(lla.Addr).String() + " ...")
	mackey := append([]byte("fahrrad/mac/"), lla.Addr...)
	prefix, err := db.Cmd("GET", mackey).Bytes()
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(prefix) != 16 {
		fmt.Printf("invalid length of prefix %x\n", prefix)
		return
	}
	fmt.Println("found prefix " + net.IP(prefix).String() + "/64")
	msgbody := []byte{0x40, 0x00, 0x07, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// Prefix option:
	op := &NDOptionPrefix{
		PrefixLength:      64,
		OnLink:            true,
		AutoConf:          true,
		ValidLifetime:     86400,
		PreferredLifetime: 14400,
		Prefix:            net.IP(prefix),
	}
	opbytes, err := op.Marshal()
	if err != nil {
		fmt.Println(err)
		return
	}
	msgbody = append(msgbody, opbytes...)

	// LLA option (FIXME: hardware address retrieval)
	localif, err := net.InterfaceByName(src.(*net.IPAddr).Zone)
	if err == nil {
		llaop := &NDOptionLLA{1, localif.HardwareAddr}
		llaopbytes, err := llaop.Marshal()
		if err == nil {
			msgbody = append(msgbody, llaopbytes...)
		} else {
			fmt.Println(err)
		}
	} else {
		fmt.Println(err)
	}

	msg := &icmp.Message{ipv6.ICMPTypeRouterAdvertisement, 0, 0, &icmp.DefaultMessageBody{msgbody}}
	mb, err := msg.Marshal(nil)
	if err != nil {
		panic(err)
	}
	// workaround wtfwtfwtf
    // RFC4861 requires the hop limit set to 255, but the default value in golang is 64
    // FIXME type casting shit
	n, err := (*pc).WriteTo(mb, src)
    pc6 := ipv6.NewPacketConn(pc)
    pc6.SetHopLimit(255)
    n, err = pc6.WriteTo(mb, nil, src)
	fmt.Printf("writeto: %v, %v\n\n", n, err)
	// FIXME: clients don't seem to accept this :(
	/*
	   _, err = pc.WriteTo(mb, src)
	   if err != nil {
	       fmt.Println(err)
	   }
	*/
}

/*
func handleRA(src net.IP, body []byte) {
	_, err := parseOptions(body[16:])
	if err != nil {
		fmt.Println(err)
	}
}

func handleNS(src net.IP, body []byte) {
	_, err := parseOptions(body[24:])
	if err != nil {
		fmt.Println(err)
	}
}

func handleNA(src net.IP, body []byte) {
	_, err := parseOptions(body[24:])
	if err != nil {
		fmt.Println(err)
	}
}

func handleRedirect(src net.IP, body []byte) {
	_, err := parseOptions(body[40:])
	if err != nil {
		fmt.Println(err)
	}
}
*/
