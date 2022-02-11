package main

import (
	"encoding/json"
	go_rpc "go-rpc"
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

}

func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err == nil {
		log.Fatal("start server: network err", err)
	}

	log.Println("start rpc server on addr", l.Addr())
	addr <- l.Addr().String()
	go_rpc.Accept(l)
}
