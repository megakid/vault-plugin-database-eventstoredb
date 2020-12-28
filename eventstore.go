package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/go-hclog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/vault/api"
	dbpluginOld "github.com/hashicorp/vault/sdk/database/dbplugin"
	dbplugin "github.com/hashicorp/vault/sdk/database/dbplugin/v5"
	"github.com/hashicorp/vault/sdk/database/helper/credsutil"
	"github.com/hashicorp/vault/sdk/database/helper/dbutil"
)

// checks that EventStore satisfied the dbplugin.Database interface
var _ dbplugin.Database = (*Eventstore)(nil)

func New() (interface{}, error) {
	db := NewEventstore()
	return dbplugin.NewDatabaseErrorSanitizerMiddleware(db, db.SecretValues), nil
}

func Run(apiTLSConfig *api.TLSConfig) error {
	dbplugin.Serve(NewEventstore())
	return nil
}

func NewEventstore() *Eventstore {

	vaultLogger := hclog.New(&hclog.LoggerOptions{})
	return &Eventstore{
		credentialProducer: &credsutil.SQLCredentialsProducer{
			DisplayNameLen: 15,
			RoleNameLen:    15,
			UsernameLen:    100,
			Separator:      "-",
		},
		logger: vaultLogger,
	}
}

// Eventstore implements dbplugin's Database interface.
type Eventstore struct {

	// The CredentialsProducer is never mutated and thus is inherently thread-safe.
	credentialProducer credsutil.CredentialsProducer

	// This protects the config from races while also allowing multiple threads
	// to read the config simultaneously when it's not changing.
	mux sync.RWMutex

	// The root credential config.
	config map[string]interface{}

	logger hclog.Logger
}

type creationStatement struct {
	Groups []string `json:"groups"`
}

func (es *Eventstore) Type() (string, error) {
	return "eventstore", nil
}

// SecretValues is used by some error-sanitizing middleware in Vault that basically
// replaces the keys in the map with the values given so they're not leaked via
// error messages.
func (es *Eventstore) SecretValues() map[string]string {
	es.mux.RLock()
	defer es.mux.RUnlock()

	replacements := make(map[string]string)
	for _, secretKey := range []string{"password", "client_key"} {
		vIfc, found := es.config[secretKey]
		if !found {
			continue
		}
		secretVal, ok := vIfc.(string)
		if !ok {
			continue
		}
		// So, supposing a password of "0pen5e5ame",
		// this will cause that string to get replaced with "[password]".
		replacements[secretVal] = "[" + secretKey + "]"
	}
	return replacements
}

// Init is called on `$ vault write database/config/:db-name`,
// or when you do a creds call after Vault's been restarted.
func (es *Eventstore) Init(ctx context.Context, config map[string]interface{}, verifyConnection bool) (map[string]interface{}, error) {

	// Validate the config to provide immediate feedback to the user.
	// Ensure required string fields are provided in the expected format.
	for _, requiredField := range []string{"username", "password", "url"} {
		raw, ok := config[requiredField]
		if !ok {
			return nil, fmt.Errorf(`%q must be provided`, requiredField)
		}
		if _, ok := raw.(string); !ok {
			return nil, fmt.Errorf(`%q must be a string`, requiredField)
		}
	}

	// Ensure optional string fields are provided in the expected format.
	for _, optionalField := range []string{"ca_cert", "ca_path", "client_cert", "client_key", "tls_server_name"} {
		raw, ok := config[optionalField]
		if !ok {
			continue
		}
		if _, ok = raw.(string); !ok {
			return nil, fmt.Errorf(`%q must be a string`, optionalField)
		}
	}

	// Check the one optional bool field is in the expected format.
	if raw, ok := config["insecure"]; ok {
		if _, ok = raw.(bool); !ok {
			return nil, errors.New(`"insecure" must be a bool`)
		}
	}

	// Test the given config to see if we can make a client.
	client, err := buildClient(config)
	if err != nil {
		return nil, errwrap.Wrapf("couldn't make client with inbound config: {{err}}", err)
	}

	// Optionally, test the given config to see if we can make a successful call.
	if verifyConnection {
		// Whether this role is found or unfound, if we're configured correctly there will
		// be no err from the call. However, if something is misconfigured, this will yield
		// an error response, which will be described in the returned error.
		if _, err := client.UserExists(ctx, "vault-test"); err != nil {
			return nil, errwrap.Wrapf("client test of getting a role failed: {{err}}", err)
		}
	}

	// Everything's working, write the new config to memory and storage.
	es.mux.Lock()
	defer es.mux.Unlock()
	es.config = config
	return es.config, nil
}

// NewUser is called on `$ vault read database/creds/:role-name`
// and it's the first time anything is touched from `$ vault write database/roles/:role-name`.
// This is likely to be the highest-throughput method for this plugin.
// func (es *Eventstore) NewUser(ctx context.Context, statements dbplugin.Statements, usernameConfig dbplugin.UsernameConfig, _ time.Time) (string, string, error) {
func (es *Eventstore) NewUser(ctx context.Context, req dbplugin.NewUserRequest) (dbplugin.NewUserResponse, error) {
	username, err := es.credentialProducer.GenerateUsername(dbpluginOld.UsernameConfig{
		DisplayName: req.UsernameConfig.DisplayName,
		RoleName:    req.UsernameConfig.RoleName,
	})

	userResponse := dbplugin.NewUserResponse{Username: username}

	if err != nil {
		return userResponse, errwrap.Wrapf(fmt.Sprintf("unable to generate username for %q: {{err}}", req.UsernameConfig), err)
	}

	password, err := es.credentialProducer.GeneratePassword()
	if err != nil {
		return userResponse, errwrap.Wrapf("unable to generate password: {{err}}", err)
	}

	//todo: update for new syntax
	stmt, err := newCreationStatement(req.Statements)
	if err != nil {
		return userResponse, errwrap.Wrapf("unable to read creation_statements: {{err}}", err)
	}

	user := &User{
		LoginName: username,
		FullName:  username,
		Password:  password,
		Groups:    stmt.Groups,
	}

	// Don't let anyone write the config while we're using it for our current client.
	es.mux.RLock()
	defer es.mux.RUnlock()

	client, err := buildClient(es.config)
	if err != nil {
		return userResponse, errwrap.Wrapf("unable to get client: {{err}}", err)
	}

	if err := client.CreateUser(ctx, username, user); err != nil {
		return userResponse, errwrap.Wrapf(fmt.Sprintf("unable to create user name %s, user %q: {{err}}", username, user), err)
	}
	return userResponse, nil
}

// UpdateUser gets called whenever we have to update a user's credetianls or lease expiration
// Depending on the action issued to/by vault, we will either see a ChangePassword or ChangeExpiration objects passed alongside a username.
func (es *Eventstore) UpdateUser(ctx context.Context, req dbplugin.UpdateUserRequest) (updateResponse dbplugin.UpdateUserResponse, err error) {

	if req.Expiration != nil {
		// nothing to do
		return updateResponse, nil
	}

	if req.Password != nil {
		return updateResponse, es.SetCredentials(ctx, req.Username, req.Password.NewPassword)

	}

	return updateResponse, err
}

// DeleteUser is called when a lease expires.
func (es *Eventstore) DeleteUser(ctx context.Context, req dbplugin.DeleteUserRequest) (dbplugin.DeleteUserResponse, error) {
	// Don't let anyone write the config while we're using it for our current client.
	es.mux.RLock()
	defer es.mux.RUnlock()

	client, err := buildClient(es.config)
	if err != nil {
		return dbplugin.DeleteUserResponse{}, errwrap.Wrapf("unable to get client: {{err}}", err)
	}

	var errs error

	// Same with the user. If it was already deleted on a previous attempt, there won't be an
	// error.
	if err := client.DeleteUser(ctx, req.Username); err != nil {
		errs = multierror.Append(errs, errwrap.Wrapf(fmt.Sprintf("unable to create user name %s: {{err}}", req.Username), err))
	}
	return dbplugin.DeleteUserResponse{}, errs
}

// SetCredentials is used to set the credentials for a database user to a
// specific username and password.
func (es *Eventstore) SetCredentials(ctx context.Context, username, password string) (err error) {
	if username == "" || password == "" {
		return errors.New("must provide both username and password")
	}

	// Don't let anyone write the config while we're using it for our current client.
	es.mux.RLock()
	defer es.mux.RUnlock()

	client, err := buildClient(es.config)
	if err != nil {
		return errwrap.Wrapf("unable to get client: {{err}}", err)
	}

	if err := client.ChangePassword(ctx, username, password); err != nil {
		return errwrap.Wrapf(fmt.Sprintf("unable to set credentials for user name %s: {{err}}", username), err)
	}
	return nil
}

func (es *Eventstore) Close() error {
	// NOOP, nothing to close.
	return nil
}

func (es *Eventstore) Initialize(ctx context.Context, req dbplugin.InitializeRequest) (dbplugin.InitializeResponse, error) {
	postInitConfig, err := es.Init(ctx, req.Config, req.VerifyConnection)
	return dbplugin.InitializeResponse{Config: postInitConfig}, err
}

func newCreationStatement(statements dbplugin.Statements) (*creationStatement, error) {
	if len(statements.Commands) == 0 {
		return nil, dbutil.ErrEmptyCreationStatement
	}
	stmt := &creationStatement{}
	if err := json.Unmarshal([]byte(statements.Commands[0]), stmt); err != nil {
		return nil, errwrap.Wrapf(fmt.Sprintf("unable to unmarshal %s: {{err}}", []byte(statements.Commands[0])), err)
	}
	return stmt, nil
}

// buildClient is a helper method for building a client from the present config,
// which is done often.
func buildClient(config map[string]interface{}) (*Client, error) {

	// We can presume these required fields are provided by strings
	// because they're validated in Init.
	clientConfig := &ClientConfig{
		Username: config["username"].(string),
		Password: config["password"].(string),
		BaseURL:  config["url"].(string),
	}

	hasTLSConf := false
	tlsConf := &TLSConfig{}

	// We can presume that if these are provided, they're in the expected format
	// because they're also validated in Init.
	if raw, ok := config["ca_cert"]; ok {
		tlsConf.CACert = raw.(string)
		hasTLSConf = true
	}
	if raw, ok := config["ca_path"]; ok {
		tlsConf.CAPath = raw.(string)
		hasTLSConf = true
	}
	if raw, ok := config["client_cert"]; ok {
		tlsConf.ClientCert = raw.(string)
		hasTLSConf = true
	}
	if raw, ok := config["client_key"]; ok {
		tlsConf.ClientKey = raw.(string)
		hasTLSConf = true
	}
	if raw, ok := config["tls_server_name"]; ok {
		tlsConf.TLSServerName = raw.(string)
		hasTLSConf = true
	}
	if raw, ok := config["insecure"]; ok {
		tlsConf.Insecure = raw.(bool)
		hasTLSConf = true
	}

	// We should only fulfill the clientConfig's TLSConfig pointer if we actually
	// want the client to use TLS.
	if hasTLSConf {
		clientConfig.TLSConfig = tlsConf
	}

	client, err := NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
}
