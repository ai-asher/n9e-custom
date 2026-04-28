// Package router exposes HTTP CRUD endpoints for the custom denoise/suppress/
// mute configuration tables. Endpoints live under /api/n9e/custom/...
//
// Auth model
//
//	Read endpoints  (GET):    require any logged-in user
//	Write endpoints (POST/PUT/DELETE): require Admin
//
// Both use N9e's existing JWT middleware via the *Router type from
// center/router — we accept it as a parameter rather than re-implementing
// JWT parsing ourselves, so password/token rotation flows stay shared.
//
// Wire-up (single line in center/center.go after the existing routers):
//
//	customRouter.Config(r, centerRouter)
package router
