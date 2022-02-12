package main

import (
	"fmt"
	go_rpc "go-rpc"
	"log"
	"net"
	"sync"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	client, _ := go_rpc.Dial("tcp", <-addr)
	defer func() {
		_ = client.Close()
	}()

	time.Sleep(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			serviceMethod := "hello.zdf"
			args := fmt.Sprintf("go rpc req %d", i)
			reply := ""
			if err := client.Call(serviceMethod, args, &reply); err != nil {
				log.Fatal(fmt.Sprintf("call %s failed %s\n", serviceMethod, err))
			}
			log.Println("client receive reply:", reply)
		}(i)
	}

	wg.Wait()
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
