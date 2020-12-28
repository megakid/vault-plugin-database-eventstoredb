# vault-plugin-database-eventstoredb

## Releases

* Binaries are packaged on the GitHub release page

## Build

* `go build` and `gox` is used to build the plugin.

## Installation

### Manual install
* Copy the binaries to your vault plugins directory
* Configure vault to load the plugin from the directory of you choosing.
    * If running vault in dev mode, you can use `-dev-plugin-dir='/plugin/directory/path'`
    * For production use-cases, consult [Vault's documentation](https://www.vaultproject.io/docs/internals/plugins#plugin-directory)

### Makefile

Alternatively, you can let `make` handle that for you.

## Register with vault
This example setup shows how to configure the plugin for a local vault dev server.

Start vault in dev mode and provide the location of your plugins directory

```
vault server -dev -log-level=debug -dev-root-token-id=root -dev-plugin-dir=./vault/plugins
```

Follow up by enabling vault's database engine.
```
vault secrets enable database
```

Ensure that vault's plugin catalog has picked-up the binary. The output should include the name of the binary you've build or downloaded. That name is later referenced in configuration, so make note of it.

```
vault plugin list database
```

Configure the plugin. Note that you can specify vault mount paths different to the plugin name, e.g. you may want to manage multiple Eventstore instances.
The example below assumes you are running Evenstore in TLS configuration. If you aren't you can drop skip the last 3 config lines

```
vault write database/config/my-eventstore-instance /
plugin_name="eventstore-db-plugin-v4" /
url="http://127.0.0.1:2113" /
allowed_roles="readonly,esuser" /
username="admin" /
password="changeit" /
ca_cert="<eventstore_install_path>/certs/ca/ca.crt" /
client_cert="<eventstore_install_path>/certs/node.crt" /
client_key="<eventstore_install_path>/certs/node.key"
```

Create roles with certain access within Evenstore. For further details, see [Eventstore's documentation](https://developers.eventstore.com/server/5.0.8/http-api/security/#access-control-lists) on users and security. The creation statement is passed as-is to Eventstore.

```
vault write database/roles/<role_name> /
db_name=<db path in write command above, i.e. my-eventstore-instance> /
creation_statements='{\"groups\":[\"someGroup\", \"someOtherGroup\"]}' /
default_ttl=1h /
max_ttl=24h
```

Now, to verify everything is setup correctly, you should be able to obtain a credentials pair. Suppose we created a role called `readonly`

```
vault read database/creds/readonly
```