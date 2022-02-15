package go_rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-rpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int
	CodecType codec.Type
	ConnectTimeout time.Duration
	HandleTimeout time.Duration
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType: codec.GobType,
	ConnectTimeout: time.Second * 10,
}

type Server struct {
	serviceMap sync.Map
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
		go s.handleRequest(cc, req, mtx, wg, DefaultOption.HandleTimeout)
	}

	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h *codec.Header
	argv, replyV reflect.Value
	mType *methodType
	svc *service
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
	req.svc, req.mType, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}

	req.argv = req.mType.newArgv()
	req.replyV = req.mType.newReplyV()
	argvInter := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvInter = req.argv.Addr().Interface()
	}

	if err = cc.ReadBody(argvInter); err != nil {
		log.Println("rpc server: read argv error", err)
	}

	return req, nil
}

func (s *Server) handleRequest(cc codec.Codec, req *request, mtx *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()

	callCh, sendCh := make(chan struct{}, 1), make(chan struct{}, 1)

	log.Println("server handle request: header:", req.h, "args:", req.argv)

	go func() {
		err := req.svc.call(req.mType, req.argv, req.replyV)
		callCh <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, mtx)
			sendCh <- struct{}{}
			return
		}

		s.sendResponse(cc, req.h, req.replyV.Interface(), mtx)
		sendCh <- struct{}{}
	}()

	if timeout == 0 {
		<-callCh
		<-sendCh
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server handle timeout must be %s", timeout)
		s.sendResponse(cc, req.h, invalidRequest, mtx)
	case <-callCh:
		<-sendCh
	}
}

func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, mtx *sync.Mutex) {
	mtx.Lock()
	defer mtx.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write rsp error", err)
	}
}

func (s *Server) Register(rcv interface{}) error {
	newS := newService(rcv)
	if _, dup := s.serviceMap.LoadOrStore(newS.name, newS); dup {
		return errors.New("rpc service already defined:" + newS.name)
	}

	return nil
}

func Register(rcv interface{}) error {
	return DefaultServer.Register(rcv)
}

func (s *Server) findService(serviceMethod string) (svc *service, mType *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}

	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svcInter, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can not find service " + serviceName)
		return
	}

	svc = svcInter.(*service)
	mType = svc.methods[methodName]
	if mType == nil {
		err = errors.New("rpc server: can not find method " + methodName)
		return
	}

	return
}
