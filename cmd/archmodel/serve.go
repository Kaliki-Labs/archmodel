package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Kaliki-Labs/archmodel/internal/explorer"
	"github.com/Kaliki-Labs/archmodel/internal/model"
	"github.com/Kaliki-Labs/archmodel/internal/serve"
)

func init() {
	register("serve", command{summary: "serve the Explorer, rebuild on file change, push reload", run: runServe})
}

func runServe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := addInputFlags(fs)
	addr := fs.String("addr", "127.0.0.1:7878", "address to listen on; there is no auth, so bind beyond localhost only on a network you trust (for example a tailnet IP)")
	ref := fs.String("ref", "", "git ref for {ref} in source.url_template")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "archmodel serve: unexpected argument %q\n", fs.Arg(0))
		return errUsage
	}
	cfg, err := in.config()
	if err != nil {
		return err
	}
	in.resolve()
	watch := []string{
		filepath.Join(in.root, cfg.Go.Dir),
		filepath.Join(in.root, cfg.API.SpecsDir),
		filepath.Join(in.root, cfg.SQL.SchemaDir),
		filepath.Join(in.root, cfg.SQL.QueriesDir),
		in.cfgPath,
	}
	if in.cover != "" {
		watch = append(watch, in.cover)
	}
	srv, err := serve.New(serve.Options{
		Build: func() (*model.Model, error) {
			// Reload the config on every build so edits to it take effect.
			c, err := in.config()
			if err != nil {
				return nil, err
			}
			return in.buildWith(c)
		},
		Render: func(m *model.Model) ([]byte, error) {
			c, err := in.config()
			if err != nil {
				return nil, err
			}
			return explorer.Render(m, explorer.Options{SourceURLTemplate: c.Source.URLTemplate, Ref: *ref, Live: true})
		},
		Watch: watch,
		Log:   stderr,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.ListenAndServe(ctx, *addr, func(a net.Addr) {
		fmt.Fprintf(stdout, "Explorer: %s\n", displayURL(a))
		if tcp, ok := a.(*net.TCPAddr); ok && !tcp.IP.IsLoopback() {
			fmt.Fprintln(stderr, "warning: listening beyond localhost with no authentication")
		}
		fmt.Fprintln(stderr, "watching for changes; Ctrl-C to stop")
	})
}

func displayURL(a net.Addr) string {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return "http://" + a.String() + "/"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port + "/"
}
