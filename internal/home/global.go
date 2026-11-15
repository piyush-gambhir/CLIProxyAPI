package home

import "sync/atomic"

var currentClient atomic.Pointer[Client]


func SetCurrent(client *Client) {
	currentClient.Store(client)
}


func Current() *Client {
	return currentClient.Load()
}


func ClearCurrent() {
	currentClient.Store(nil)
}


func ClearCurrentIf(client *Client) {
	if client != nil {
		currentClient.CompareAndSwap(client, nil)
	}
}
