package main

import (
	"context"
	"fmt"
	go_rpc "go-rpc"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

func main() {
	log.SetFlags(0)
	ch := make(chan string)
	go call(ch)
	startHttpServer(ch)
}

func call(addCh chan string) {
	client, _ := go_rpc.DialHTTP("tcp", <-addCh)
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
			ctx, _ := context.WithTimeout(context.Background(), time.Second)
			if err := client.Call(ctx, serviceMethod, args, &reply); err != nil {
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

func startHttpServer(addr chan string) {
	var age Age
	if err := go_rpc.Register(&age); err != nil {
		log.Fatal("register service err", err)
	}

	go_rpc.HandleHTTP()

	l, _ := net.Listen("tcp", ":9999")

	log.Println("start http server on addr", l.Addr())

	addr <- l.Addr().String()

	_ = http.Serve(l, nil)
}

type Age int

type AgeArgs struct {
	Num1, Num2 int
}

func (f Age) Sum(args AgeArgs, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}
