// Command acp-client bridges stdio to an agent exposed by acp-server, so an ACP
// client such as Zed can run the remote agent as if it were local:
//
//	acp-client <ticket>
//
// Without a ticket it prints its ID, for acp-server -allow. Its key is saved
// in -key, so the ID stays the same.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	irohacp "github.com/carsonfarmer/iroh-acp-go"
	"github.com/tmc/go-iroh/iroh"
)

func main() {
	dir, _ := os.UserConfigDir()
	keyFile := flag.String("key", filepath.Join(dir, "iroh-acp", "client.key"), "key `file`, created if missing")
	flag.Parse()
	sk, err := irohacp.LoadKey(*keyFile)
	if err != nil {
		log.Fatal(err)
	}
	if flag.NArg() == 0 {
		fmt.Println(sk.Public())
		return
	}
	ctx := context.Background()
	ep, err := irohacp.Bind(ctx, iroh.WithSecretKey(sk))
	if err != nil {
		log.Fatal(err)
	}
	c, err := irohacp.Dial(ctx, ep, flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		_, _ = io.Copy(c, os.Stdin)
		_ = c.(interface{ CloseWrite() error }).CloseWrite()
	}()
	_, err = io.Copy(os.Stdout, c)
	_ = ep.Shutdown(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
