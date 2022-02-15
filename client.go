package go_rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-rpc/codec"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type Call struct {
	Seq uint64
	ServiceMethod string
	Error error
	Args interface{}
	Reply interface{}
	Done chan *Call
}

func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc codec.Codec
	opt *Option
	sendMtx *sync.Mutex
	mtx *sync.Mutex
	seq uint64
	header *codec.Header
	pending map[uint64]*Call
	closing bool
	shutdown bool
}

type clientResult struct {
	client *Client
	err error
}

type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

func DialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)

		// 防止goroutine泄漏
		select {
		case ch <- clientResult{client: client, err: err}:
		default:
		}
	}()

	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

var _ io.Closer = (*Client)(nil)

var ErrShutDown = errors.New("connection is shutdown")

func (client *Client) Close() error {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.closing {
		return ErrShutDown
	}

	client.closing = true
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	return !client.closing && !client.shutdown
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	if client.closing || client.shutdown {
		return 0, ErrShutDown
	}

	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	call := client.pending[seq]
	delete(client.pending, call.Seq)
	return call
}

func (client *Client) terminateCalls(err error) {
	client.sendMtx.Lock()
	defer client.sendMtx.Unlock()
	client.mtx.Lock()
	defer client.mtx.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) receive() {
	var err error

	for err == nil {
		var header codec.Header
		if err = client.cc.ReadHeader(&header); err != nil {
			break
		}

		call := client.removeCall(header.Seq)
		switch {
		case call == nil:
			err = client.cc.ReadBody(nil)
		case header.Error != "":
			call.Error = fmt.Errorf(header.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body error" + err.Error())
			}
			call.done()
		}
	}

	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid code type %s", opt.CodecType)
		log.Println("rpc client: code error", err)
		return nil, err
	}

	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options err", err)
		_ = conn.Close()
		return nil, err
	}

	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq: 1,
		cc: cc,
		opt: opt,
		pending: make(map[uint64]*Call),
		header: new(codec.Header),
		mtx: new(sync.Mutex),
		sendMtx: new(sync.Mutex),
	}

	go client.receive()

	return client
}

func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}

	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}

	return opt, nil
}

func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	return DialTimeout(NewClient, network, address, opts...)
}

func (client *Client) send(call *Call) {
	client.sendMtx.Lock()
	defer client.sendMtx.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	if err := client.cc.Write(client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	call := &Call{
		ServiceMethod: serviceMethod,
		Args: args,
		Reply: reply,
		Done: done,
	}
	go client.send(call)
	return call
}

func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call))

	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}
