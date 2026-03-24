package cli

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const nat64Prefix = "64:ff9b::"

func toNAT64(ipv4 net.IP) string {
	v4 := ipv4.To4()
	return fmt.Sprintf("%s%02x%02x:%02x%02x", nat64Prefix, v4[0], v4[1], v4[2], v4[3])
}

func dialInNamespace(addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		// Raw IPv4 → convert to NAT64
		host = toNAT64(ip)
		addr = net.JoinHostPort(host, port)
	}

	return net.Dial("tcp6", addr)
}

func NewDialCommand() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:    "dial",
		Short:  "Internal helper — bridges stdin/stdout to a TCP connection",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := dialInNamespace(addr)
			if err != nil {
				return err
			}
			defer conn.Close()

			errc := make(chan error, 2)
			go func() {
				_, err := io.Copy(conn, os.Stdin)
				errc <- err
			}()
			go func() {
				_, err := io.Copy(os.Stdout, conn)
				errc <- err
			}()

			if err := <-errc; err != nil {
				logrus.WithError(err).Debug("dial bridge ended")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "Target address (host:port)")
	cmd.MarkFlagRequired("addr")

	return cmd
}
