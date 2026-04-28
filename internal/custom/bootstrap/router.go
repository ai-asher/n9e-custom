package bootstrap

import (
	centerRouter "github.com/ccfos/nightingale/v6/center/router"
	customRouter "github.com/ccfos/nightingale/v6/internal/custom/router"
	"github.com/ccfos/nightingale/v6/pkg/ctx"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/logger"
)

// InstallRouter mounts the /api/n9e/custom/* CRUD endpoints onto the
// existing N9e gin engine. Call this from center.go AFTER N9e's own
// routers have been configured — order doesn't matter for the routes
// themselves (gin handles them as a flat trie) but keeping a consistent
// order makes the boot log easier to read.
//
// The single-line wiring contract from center.go:
//
//	customBootstrap.InstallRouter(c, centerRouter, r)
//
// where r is the *gin.Engine already passed to centerRouter.Config(r).
func InstallRouter(c *ctx.Context, parent *centerRouter.Router, engine *gin.Engine) {
	if c == nil || parent == nil || engine == nil {
		logger.Errorf("custom/bootstrap: nil arg to InstallRouter; routes NOT installed")
		return
	}
	cr := customRouter.New(c, parent)
	cr.Config(engine)
	logger.Infof("custom/bootstrap: HTTP CRUD routes mounted at /api/n9e/custom/*")
}
