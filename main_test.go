package main

import "testing"

func TestSetupRouter_RegistersRestartRoute(t *testing.T) {
	router := setupRouter()
	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/restart" {
			return
		}
	}
	t.Fatal("POST /api/restart route is not registered")
}
