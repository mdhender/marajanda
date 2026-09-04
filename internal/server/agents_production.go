// Copyright (c) 2026 Michael D Henderson.

//go:build production

package server

import "net/http"

func registerAgentRoutes(*http.ServeMux, *application, string) {}
