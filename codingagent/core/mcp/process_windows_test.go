package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestStdioTransportHasNoConsole(t *testing.T) {
	transport, err := buildTransport(ServerConfig{Command: "example-server"}, "")
	if err != nil {
		t.Fatal(err)
	}
	cmd := transport.(*sdk.CommandTransport).Command
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("MCP command can create a console window")
	}
}
