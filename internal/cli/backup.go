package cli

import (
	"fmt"

	"github.com/MrZudro/badomens/internal/backup"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

func newBackupCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Copias de seguridad por instancia",
	}
	cmd.AddCommand(
		newBackupCreateCmd(e),
		newBackupListCmd(e),
		newBackupRestoreCmd(e),
		newBackupPruneCmd(e),
	)
	return cmd
}

func (e *Env) backupBackend() (*backup.Backend, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	return &backup.Backend{DataDir: cfg.DataDir}, nil
}

func newBackupCreateCmd(e *Env) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea una copia de seguridad (detiene la instancia si está corriendo)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("backup create", err), "")
			}
			b, err := e.backupBackend()
			if err != nil {
				return e.finish(result.FromError("backup create", err), "")
			}
			var info backup.Info
			phases, err := e.withStopped(cmd, spec.Slug, func() error {
				var berr error
				info, berr = b.Create(spec.Slug, name)
				return berr
			})
			if err != nil {
				res := result.FromError("backup create", err)
				res.WithPhases(phases)
				return e.finish(res, "")
			}
			res := result.OK("backup create", "backup creado: "+info.Name, map[string]any{
				"name": info.Name, "path": info.Path, "sha256": info.Sha256, "size": info.Size,
			}).WithPhases(phases)
			return e.finish(res, fmt.Sprintf("Backup %s creado\nsha256: %s", info.Name, info.Sha256))
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "nombre del backup (vacío = marca de tiempo)")
	return cmd
}

func newBackupListCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los backups de una instancia",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("backup list", err), "")
			}
			b, err := e.backupBackend()
			if err != nil {
				return e.finish(result.FromError("backup list", err), "")
			}
			list, err := b.List(spec.Slug)
			if err != nil {
				return e.finish(result.FromError("backup list", err), "")
			}
			human := fmt.Sprintf("Backups de %s (%d):", spec.Slug, len(list))
			if len(list) == 0 {
				human += "\n  (sin backups)"
			}
			for _, info := range list {
				human += fmt.Sprintf("\n  %-22s %8d bytes", info.Name, info.Size)
			}
			return e.finish(result.OK("backup list", "backups", map[string]any{"instance": spec.Slug, "backups": list}), human)
		},
	}
}

func newBackupRestoreCmd(e *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <nombre>",
		Short: "Restaura un backup sobre la instancia (sustituye el estado actual)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("backup restore", err), "")
			}
			ok, err := e.confirm("restaurar en la instancia", args[0]+" → "+spec.Slug, yes)
			if err != nil || !ok {
				return e.finish(result.FromError("backup restore", errx.Usage("operación cancelada", "")), "operación cancelada")
			}
			b, err := e.backupBackend()
			if err != nil {
				return e.finish(result.FromError("backup restore", err), "")
			}
			res := mutatingResult("backup restore", spec.Slug, func() error {
				return b.Restore(spec.Slug, args[0])
			}, e, cmd)
			return e.finish(res, humanFor(res))
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirmar sin preguntar")
	return cmd
}

func newBackupPruneCmd(e *Env) *cobra.Command {
	var keep int
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Elimina los backups más antiguos dejando solo los N más recientes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("backup prune", err), "")
			}
			b, err := e.backupBackend()
			if err != nil {
				return e.finish(result.FromError("backup prune", err), "")
			}
			removed, err := b.Prune(spec.Slug, keep)
			if err != nil {
				return e.finish(result.FromError("backup prune", err), "")
			}
			human := fmt.Sprintf("%d backup(s) eliminados de %s", len(removed), spec.Slug)
			return e.finish(result.OK("backup prune", human, map[string]any{"removed": removed}), human)
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 5, "cuántos backups conservar")
	return cmd
}
