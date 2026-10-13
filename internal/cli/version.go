package cli

import (
	"github.com/manuelserver/badomens/internal/result"
	"github.com/manuelserver/badomens/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión del binario",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res := result.OK("version", "badomens "+version.Version, map[string]any{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
			})
			return e.finish(res, version.String())
		},
	}
}
