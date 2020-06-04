package main

import (
	"log"
	"os"

	"github.com/hashicorp/vault/api"
	elasticsearch "github.com/megakid/vault-plugin-database-eventstoredb"
)

func main() {
	apiClientMeta := &api.PluginAPIClientMeta{}
	flags := apiClientMeta.FlagSet()
	flags.Parse(os.Args[1:])

	if err := elasticsearch.Run(apiClientMeta.GetTLSConfig()); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
