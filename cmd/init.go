package cmd

import (
	_ "embed"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	cfg "github.com/federizer/fedemail/internal/config"
	"github.com/federizer/fedemail/internal/database"
)

var (
	//go:embed sql/init_database.sql
	initDabaseStmt string
)

var initializeCmd = &cobra.Command{
	Use: "init",
	Run: func(cmd *cobra.Command, args []string) {
		err := initDb(config)
		if err != nil {
			logrus.WithError(err).Fatal("unable to create the database")
		}
		logrus.Info("database created")
	},
}

func initDb(config *cfg.Config) error {
	db, err := database.ConnectAsAdmin(config)
	if err != nil {
		return err
	}
	defer db.Close()

	stmt := fmt.Sprintf(initDabaseStmt, config.User.Username, config.User.Password, config.User.DatabaseName)
	_, err = db.Exec(stmt)
	if err != nil {
		return err
	}

	return nil
}
