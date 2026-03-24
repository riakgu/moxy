package cli

import (
	"io"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func NewDialCommand() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:    "dial",
		Short:  "Internal helper — bridges stdin/stdout to a TCP connection",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := net.Dial("tcp", addr)
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
