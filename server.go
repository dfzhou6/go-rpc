package go_rpc

import (
	"encoding/json"
	"fmt"
	"go-rpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int
	CodecType codec.Type
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType: codec.GobType,
}

type Server struct {

}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error", err)
			break
		}

		go s.ServeConn(conn)
	}
}

func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	var opt Option

	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc option: decode error", err)
		return
	}

	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc option: invalid magicnumber %x\n", opt.MagicNumber)
		return
	}

	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codectype %s\n", opt.CodecType)
		return
	}

	s.serveCodec(f(conn))
}

var invalidRequest = struct {}{}

func (s *Server) serveCodec(cc codec.Codec) {
	mtx := new(sync.Mutex)
	wg := new(sync.WaitGroup)

	for {
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}

			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, mtx)
			continue
		}

		wg.Add(1)
		go s.handleRequest(cc, req, mtx, wg)
	}

	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h *codec.Header
	argv, replyV reflect.Value
}

func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header

	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error", err)
		}
		return nil, err
	}

	return &h, nil
}

func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	req := &request{h: h}
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv error", err)
	}

	return req, nil
}

func (s *Server) handleRequest(cc codec.Codec, req *request, mtx *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Println(req.h, req.argv.Elem())
	req.replyV = reflect.ValueOf(fmt.Sprintf("rpc rsp %d", req.h.Seq))
	s.sendResponse(cc, req.h, req.replyV.Interface(), mtx)
}

func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, mtx *sync.Mutex) {
	mtx.Lock()
	defer mtx.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write rsp error", err)
	}
}
