// Command acp-server exposes a stdio ACP agent to the iroh peers it allows. It
// prints an endpoint ticket, then runs one agent process per connection:
//
//	acp-server -allow <client-id> npx -y @zed-industries/claude-code-acp
//
// Its key is saved in -key, so tickets keep working across restarts.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	irohacp "github.com/carsonfarmer/iroh-acp-go"
	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"
)

func main() {
	dir, _ := os.UserConfigDir()
	keyFile := flag.String("key", filepath.Join(dir, "iroh-acp", "server.key"), "key `file`, created if missing")
	var allow []key.EndpointID
	flag.Func("allow", "client `id` to serve, as printed by acp-client (repeatable)", func(s string) error {
		id, err := key.ParseEndpointID(s)
		allow = append(allow, id)
		return err
	})
	flag.Parse()
	if flag.NArg() == 0 || len(allow) == 0 {
		log.Fatal("usage: acp-server -allow <client-id> [-allow ...] <agent-command> [args...]")
	}
	sk, err := irohacp.LoadKey(*keyFile)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	ep, err := irohacp.Bind(ctx, iroh.WithSecretKey(sk))
	if err != nil {
		log.Fatal(err)
	}
	online, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = ep.Online(online) // without a home relay the ticket has direct addresses only
	cancel()
	fmt.Println(endpointticket.Encode(ep.Addr()))
	log.Fatal(irohacp.Serve(ctx, ep, irohacp.AllowIDs(allow...), func(c net.Conn) {
		cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.WaitDelay = c, c, os.Stderr, time.Second
		log.Print("agent exited: ", cmd.Run())
	}))
}
