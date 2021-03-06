package local

import (
	"encoding/json"
	"time"

	"common"
	"github.com/gorilla/websocket"
	"inbound"
)

var (
	consoleWSUrl = "wss://www.console.com/v1/ws"
	writeWait    = 60 * time.Second
	pingWait     = 10 * time.Second
	wsDialer     = websocket.Dialer{
		Subprotocols:    []string{"p1", "p2"},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

type Connection struct {
	conn *websocket.Conn
	msg  chan []byte
	done chan bool
}

func (c *Connection) pingHandler(s string) error {
	c.conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(pingWait))
	return nil
}

func (c *Connection) handleWS(msg []byte) []byte {
	var m common.WebsocketMessage
	err := json.Unmarshal(msg, &m)
	if err != nil {
		res := common.WebsocketMessage{
			Cmd: common.CMD_ERROR,
		}
		r, _ := json.Marshal(res)
		return r
	}

	switch m.Cmd {
	case common.CMD_START_REVERSE_SSH:
		m.Cmd = common.CMD_REVERSE_SSH_STARTED
	case common.CMD_STOP_REVERSE_SSH:
		m.Cmd = common.CMD_REVERSE_SSH_STOPPED
	case common.CMD_NEW_RULES:
		if inbound.IsInBoundModeEnabled("redir") {
			go updateRedirFirewallRules()
		}
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	case common.CMD_ADD_SERVER:
		addServer(m.WParam)
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	case common.CMD_DEL_SERVER:
		removeServer(m.WParam)
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	case common.CMD_SET_PORT:
		defaultPort = m.WParam
		changePort()
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	case common.CMD_SET_KEY:
		defaultKey = m.WParam
		changeKeyMethod()
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	case common.CMD_SET_METHOD:
		defaultMethod = m.WParam
		changeKeyMethod()
		m.Cmd = common.CMD_RESPONSE
		m.WParam = "ok"
	}

	r, _ := json.Marshal(m)
	return r
}

func (c *Connection) readWS() error {
	for {
		_, p, err := c.conn.ReadMessage()
		// don't worry, if write goroutine exits abnormally,
		// the connection will be closed,
		// then this read operation will be interrupted abnormally
		if err != nil {
			common.Error("websocket reading message failed", err)
			c.done <- true
			return err
		}
		c.msg <- p
	}
	return nil
}

func (c *Connection) writeWS() error {
	for {
		select {
		case t := <-c.msg:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.conn.WriteMessage(websocket.BinaryMessage, c.handleWS(t))
			if err != nil {
				common.Error("websocket writing message failed", err)
				return err
			}
			common.Debug("websocket message sent")
		case <-c.done:
			common.Debug("websocket done")
			return websocket.ErrCloseSent
		}
	}
	return nil
}

func (c *Connection) SendMsg(cmd int, wParam string, lParam string) {
	msg := &common.WebsocketMessage{
		Cmd:    cmd,
		WParam: wParam,
		LParam: lParam,
	}
	m, err := json.Marshal(msg)
	if err != nil {
		common.Error("marshalling message failed", err)
		return
	}

	c.msg <- m
}

func (c *Connection) connectWS() {
	var err error
	c.conn, _, err = wsDialer.Dial(consoleWSUrl, nil)
	if err != nil {
		common.Error("websocket dialing failed to", consoleWSUrl, err)
		return
	}
	defer c.conn.Close()
	c.conn.SetPingHandler(c.pingHandler)
	common.Debug("websocket connected to", consoleWSUrl)

	c.SendMsg(common.CMD_AUTH, config.Generals.Token, "")

	go c.readWS()
	c.writeWS()
}

func consoleWS() {
	c := &Connection{
		msg:  make(chan []byte, 1),
		done: make(chan bool),
	}

	for {
		c.connectWS()
		time.Sleep(10 * time.Second)
	}
}
