package sockjs

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"
)

var (
	wsServer = websocket.Server{Handler: wsHandler, Handshake: WSHandshake}
	handlers = make(map[*http.Request]*handler)
)

func wsHandler(conn *websocket.Conn) {
	req := conn.Request()

	h, ok := handlers[req]
	if ok {
		delete(handlers, req)
	} else {
		conn.Close()
		return
	}

	sessID, _ := h.parseSessionID(req.URL)
	sess := newSession(sessID, h.options.DisconnectDelay, h.options.HeartbeatDelay)

	if h.handlerFunc != nil {
		go h.handlerFunc(sess)
	}

	receiver := newWsReceiver(conn)
	sess.attachReceiver(receiver)
	readCloseCh := make(chan struct{})

	go func() {
		for {
			var d []string
			if err := websocket.JSON.Receive(conn, &d); err != nil {
				close(readCloseCh)
				return
			}

			sess.accept(d...)
		}
	}()

	select {
	case <-readCloseCh:
	case <-receiver.doneNotify():
	}

	conn.Close()
	sess.close()
}

func WSHandshake(config *websocket.Config, req *http.Request) error {
	// accept all connections by default
	return nil
}

func (h *handler) sockjsWebsocket(rw http.ResponseWriter, req *http.Request) {
	handlers[req] = h
	wsServer.ServeHTTP(rw, req)
}

type wsReceiver struct {
	conn    *websocket.Conn
	closeCh chan struct{}
}

func newWsReceiver(conn *websocket.Conn) *wsReceiver {
	return &wsReceiver{
		conn:    conn,
		closeCh: make(chan struct{}),
	}
}

func (w *wsReceiver) sendBulk(messages ...string) {
	if len(messages) > 0 {
		str := fmt.Sprintf("a[%s]", strings.Join(transform(messages, quote), ","))
		w.sendFrame(str)
	}
}

func (w *wsReceiver) sendFrame(frame string) {
	bytes := []byte(frame)
	if _, err := w.conn.Write(bytes); err != nil {
		w.close()
	}
}

func (w *wsReceiver) close() {
	select {
	case <-w.closeCh: // already closed
	default:
		close(w.closeCh)
	}
}

func (w *wsReceiver) canSend() bool {
	select {
	case <-w.closeCh: // already closed
		return false
	default:
		return true
	}
}

func (w *wsReceiver) doneNotify() <-chan struct{} {
	return w.closeCh
}

func (w *wsReceiver) interruptedNotify() <-chan struct{} {
	return nil
}
