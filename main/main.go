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

			serviceMethod := "Age.Sum"
			args := AgeArgs{Num1: i + 1, Num2: i + 2}
			var reply int
			if err := client.Call(serviceMethod, args, &reply); err != nil {
				log.Fatal(fmt.Sprintf("call %s failed %s\n", serviceMethod, err))
			}

			log.Println("client receive reply:", reply)
		}(i)
	}

	wg.Wait()
}

func startServer(addr chan string) {
	var age Age
	if err := go_rpc.Register(&age); err != nil {
		log.Fatal("register service err", err)
	}

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("start server: network err", err)
	}

	log.Println("start rpc server on addr", l.Addr())
	addr <- l.Addr().String()
	go_rpc.Accept(l)
}

type Age int

type AgeArgs struct {
	Num1, Num2 int
}

func (f Age) Sum(args AgeArgs, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}
