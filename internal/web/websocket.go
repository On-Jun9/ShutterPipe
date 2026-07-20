package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: isSameOriginRequest,
}

const websocketWriteTimeout = 10 * time.Second

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	stop       chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	defer close(h.done)
	for {
		select {
		case <-h.stop:
			h.mu.Lock()
			for client := range h.clients {
				delete(h.clients, client)
				client.close()
			}
			h.mu.Unlock()
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.close()
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			// broadcast 중에 block된 클라이언트를 map에서 제거할 수 있으므로 write lock을 사용한다.
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					delete(h.clients, client)
					client.close()
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) Shutdown() {
	h.stopOnce.Do(func() { close(h.stop) })
	<-h.done
}

type Client struct {
	hub            *Hub
	conn           *websocket.Conn
	send           chan []byte
	unregisterOnce sync.Once
	closeOnce      sync.Once
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.send)
		if c.conn != nil {
			c.conn.Close()
		}
	})
}

func (c *Client) requestUnregister() {
	c.unregisterOnce.Do(func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.stop:
		}
	})
}

func (c *Client) readPump() {
	defer c.requestUnregister()

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	defer c.requestUnregister()

	for message := range c.send {
		if err := c.conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout)); err != nil {
			return
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	select {
	case client.hub.register <- client:
	case <-client.hub.stop:
		conn.Close()
		return
	}

	go client.readPump()
	go client.writePump()
}
