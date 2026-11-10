package notify

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
)

// Embed the existing notification artwork so standalone binaries work offline.
//
//go:embed icons/codex.png
var codexIcon []byte

//go:embed icons/claude.png
var claudeIcon []byte

//go:embed icons/omo.png
var omoIcon []byte

type desktopIdentity struct {
	AppID       string
	DisplayName string
	IconFile    string
	Icon        []byte
}

func identityForAgent(agent string) desktopIdentity {
	identity := desktopIdentity{AppID: "AgentNotify.Desktop", DisplayName: "Agent Notify"}
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "codex":
		identity.AppID += ".Codex"
		identity.DisplayName, identity.Icon = "Codex Notify", codexIcon
	case "claude", "claude_code":
		identity.AppID += ".Claude"
		identity.DisplayName, identity.Icon = "Claude Notify", claudeIcon
	case "omo", "opencode":
		identity.AppID += ".OMO"
		identity.DisplayName, identity.Icon = "OMO Notify", omoIcon
	}
	if len(identity.Icon) > 0 {
		// A content-based name lets existing notifications keep their old icon
		// while an update installs new artwork without rewriting a cached file.
		identity.IconFile = fmt.Sprintf("%x.png", sha256.Sum256(identity.Icon))
	}
	return identity
}
