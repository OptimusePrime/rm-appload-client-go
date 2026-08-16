/*
Package appload_client

This is a library for writing backends for reMarkable AppLoad applications.
It provides a fully fledged message exchange mechanism for communication with the app's UI
*/
package appload_client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
)

const MaxMessageLength = 10485760
const MessageHeaderLength = 8

const MessageSystemTerminate = 0xFFFFFFFF
const MessageSystemNewCoordinator = 0xFFFFFFFE

var ErrFailedConnectingToSocket = errors.New("failed to establish a connection to AppLoad Unix socket")
var ErrFailedToReadFromSocket = errors.New("failed to read from AppLoad Unix socket")
var ErrMessageTooLong = fmt.Errorf("AppLoad message exceeds maximum length (%v bytes)", MaxMessageLength)
var ErrFailedSendingMessageHeader = errors.New("failed sending message header to AppLoad Unix socket")
var ErrFailedSendingMessageContent = errors.New("failed sending message content to AppLoad Unix socket")
var ErrFailedToApplyOption = errors.New("failed to apply backend option")

type AppLoadBackend struct {
	handlers   map[MessageType]MessageHandler
	socketPath string
	Socket     net.Conn
}

func NewAppLoadBackend() AppLoadBackend {
	backend := AppLoadBackend{}
	backend.handlers = map[MessageType]MessageHandler{}
	backend.socketPath = os.Args[1]

	return backend
}

type BackendOption func(backend *AppLoadBackend, sender MessageSender) error

func WithCleanup(cleanup func()) BackendOption {
	return func(backend *AppLoadBackend, _ MessageSender) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-ctx.Done()

			stop()
			cleanup()
		}()

		return nil
	}
}

func WithSetup(setup func(backend *AppLoadBackend, sender MessageSender) error) BackendOption {
	return func(backend *AppLoadBackend, sender MessageSender) error {
		return setup(backend, sender)
	}
}

func (b *AppLoadBackend) Run(opts ...BackendOption) error {
	conn, err := net.Dial("unixpacket", b.socketPath)
	if err != nil {
		return wrapErrWithColon(ErrFailedConnectingToSocket, err)
	}

	b.Socket = conn
	defer b.Socket.Close()

	sender := MessageSender{
		backend: b,
	}

	for _, opt := range opts {
		err = opt(b, sender)
		if err != nil {
			return wrapErrWithColon(ErrFailedToApplyOption, err)
		}
	}

	for {
		headerBuf := make([]byte, MessageHeaderLength)
		_, err = io.ReadFull(conn, headerBuf)
		if err != nil {
			return wrapErrWithColon(ErrFailedToReadFromSocket, err)
		}

		header := new(MessageHeader)
		header.Deserialize(headerBuf)

		if header.length > MaxMessageLength {
			return ErrMessageTooLong
		}

		if header.msgType == MessageSystemTerminate {
			break
		}

		msgBuf := make([]byte, header.length)
		_, err = io.ReadFull(conn, msgBuf)
		if err != nil {
			return wrapErrWithColon(ErrFailedToReadFromSocket, err)
		}

		handler := b.handlers[header.msgType]
		if handler == nil {
			continue
		}
		handler(msgBuf, sender)
	}

	return nil
}

func (b *AppLoadBackend) RegisterMessageHandler(msgType MessageType, handler MessageHandler) {
	b.handlers[msgType] = handler
}

type MessageType uint32

type MessageHeader struct {
	msgType MessageType
	length  uint32
}

func (mh *MessageHeader) Serialize() []byte {
	buf := make([]byte, MessageHeaderLength)

	binary.NativeEndian.PutUint32(buf[0:4], uint32(mh.msgType))
	binary.NativeEndian.PutUint32(buf[4:], mh.length)

	return buf
}

func (mh *MessageHeader) Deserialize(headerBuf []byte) {
	mh.msgType = MessageType(binary.NativeEndian.Uint32(headerBuf[0:4]))
	mh.length = binary.NativeEndian.Uint32(headerBuf[4:])
}

type MessageSender struct {
	backend *AppLoadBackend
}

func (s MessageSender) SendMessage(msgType MessageType, content []byte) error {
	if len(content) > MaxMessageLength {
		return ErrMessageTooLong
	}

	header := MessageHeader{
		msgType: msgType,
		length:  uint32(len(content)),
	}
	_, err := s.backend.Socket.Write(header.Serialize())
	if err != nil {
		return wrapErrWithColon(ErrFailedSendingMessageHeader, err)
	}

	_, err = s.backend.Socket.Write(content)
	if err != nil {
		return wrapErrWithColon(ErrFailedSendingMessageContent, err)
	}

	return nil
}

type MessageHandler func(msgContent []byte, sender MessageSender)

func wrapErrWithColon(errs ...error) error {
	if len(errs) == 1 {
		return errs[0]
	}

	newErr := fmt.Errorf("%w: %w", errs[0], errs[1])
	if len(errs) < 3 {
		return newErr
	}

	for _, err := range errs[2:] {
		newErr = fmt.Errorf("%w: %w", newErr, err)
	}

	return newErr
}
