# vault-plugin-database-eventstoredb

## Releases

* Binaries are packaged on the GitHub release page

## Build

* `go build` and `gox` is used to build the plugin.

## Installation

* Copy the binaries to your vault plugins directory
* Configure vault to load the plugin from the directory of you choosing.
    * If running vault in dev mode, you can use `-dev-plugin-dir='/plugin/directory/path'`
    * For production use-cases, consult [Vault's documentation](https://www.vaultproject.io/docs/internals/plugins#plugin-directory)

## Register with vault
This example setup shows how to configure the plugin for a local vault dev server.

```
vault secrets enable database
```

```
vault write database/config/es-dbplugin-v4api /
plugin_name="eventstore-db-plugin-v4" /
url="http://127.0.0.1:2113" /
allowed_roles="readonly,esuser" /
username="admin" /
password="changeit" /
ca_cert="<eventstore_install_path>/certs/ca/ca.crt" /
client_cert="<eventstore_install_path>/certs/node.crt" /
client_key="<eventstore_install_path>/certs/node.key"
```

```
vault write database/roles/<role_name> `
db_name=<db path in write command above, i.e. es-dbplugin-v4api> `
creation_statements='{\"groups\":[\"someGroup\", \"someOtherGroup\"]}' `
default_ttl=1h `
max_ttl=24h
```
