package main

import (
	"log"
	"os"

	"github.com/hashicorp/vault/api"
	eventstore "github.com/megakid/vault-plugin-database-eventstoredb"
)

func main() {
	apiClientMeta := &api.PluginAPIClientMeta{}
	flags := apiClientMeta.FlagSet()
	flags.Parse(os.Args[1:])

	if err := eventstore.Run(apiClientMeta.GetTLSConfig()); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
