package main

import (
	"encoding/json"
	"fmt"
	go_rpc "go-rpc"
	"go-rpc/codec"
	"log"
	"net"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	conn, _ := net.Dial("tcp", <-addr)
	defer func() {
		_ = conn.Close()
	}()

	time.Sleep(time.Second)

	_ = json.NewEncoder(conn).Encode(go_rpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "zdf.laugh",
			Seq: uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("go rpc req %d", h.Seq))
		_ = cc.ReadHeader(h)
		reply := ""
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}

func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("start server: network err", err)
	}

	log.Println("start rpc server on addr", l.Addr())
	addr <- l.Addr().String()
	go_rpc.Accept(l)
}
