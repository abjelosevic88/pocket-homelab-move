package server

import (
	"context"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-move/internal/notify"
)

// notifyDone tells the person a long piece of work finished, if they want that.
func (s *Server) notifyDone(ctx context.Context, uid int64, title, body, page string) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil || !st.NotifyDone || st.NotifyURL == "" {
		return
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = strings.TrimRight(s.cfg.BaseURL, "/") + page
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"package"}, Link: link}); err != nil {
		s.log.Debug("notify failed", "err", err)
	}
}
