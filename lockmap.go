package main

import (
	"golang.org/x/net/websocket"
	"sync"
)

func NewLockMapSS() *LockMapSS {
	return &LockMapSS{map[string]string{}, &sync.RWMutex{}}
}

type LockMapSS struct {
	Map  map[string]string
	Lock *sync.RWMutex
}

func (d LockMapSS) Set(k string, v string) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	d.Map[k] = v
}
func (d LockMapSS) Delete(k string) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	delete(d.Map, k)
}

//======================================================================================
func NewLockMapSC() *LockMapSC {
	return &LockMapSC{map[string]*websocket.Conn{}, &sync.RWMutex{}}
}

type LockMapSC struct {
	Map  map[string]*websocket.Conn
	Lock *sync.RWMutex
}

func (d LockMapSC) Set(k string, v *websocket.Conn) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	d.Map[k] = v
}
func (d LockMapSC) Delete(k string) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	delete(d.Map, k)
}

//======================================================================================
func NewLockMapCS() *LockMapCS {
	return &LockMapCS{map[*websocket.Conn]string{}, &sync.RWMutex{}}
}

type LockMapCS struct {
	Map  map[*websocket.Conn]string
	Lock *sync.RWMutex
}

func (d LockMapCS) Set(k *websocket.Conn, v string) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	d.Map[k] = v
}
func (d LockMapCS) Delete(k *websocket.Conn) {
	d.Lock.Lock()
	defer d.Lock.Unlock()
	delete(d.Map, k)
}

//======================================================================================
