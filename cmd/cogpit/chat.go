package main

import (
	"log/slog"

	"github.com/guided-traffic/cogpit/internal/agent"
	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/engine"
)

// installChat wires the built-in agent (internal/agent, D-10) into the session, which the window's
// chat column reaches through Session.Chat: its LLM connections, conversations and API keys in
// Cogpit's configuration directory (agent.ConnectionsFile, agent.ConversationsFile,
// agent.KeysFile, each owner-only; no keychain, the owner's decision). Without the directory they
// last for the run.
func installChat(s *engine.Session, logger *slog.Logger) {
	dir := ""
	if dirs, err := app.UserDirs(); err == nil {
		dir = dirs.Config
	} else {
		logger.Warn("the chat's connections and conversations last for this session", "err", err)
	}
	s.SetChatAgent(agent.New(agent.Options{Session: s, Dir: dir, Logger: logger}))
}
