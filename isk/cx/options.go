package cx

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

// Options describes all runtime options for the API
type Options struct {
	Production, Debug, HTTPS                bool
	Port, CacheTime, CacheResp, MaxPrefRows int
	CharacterID, MaxPrefLen, MaxPatternLen  int32
	Hostname, ESI, AppSecret                string
	DB                                      *DBOptions
	Auth                                    *oauth2.Config
}

// DBOptions describes our database connection
type DBOptions struct {
	Host, User, Password, Name, Mode string
}

func readAuthConf(ctx context.Context, filePath string) *oauth2.Config {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Println("Warning: no oauth config found. no one can sign up")
		return nil
	}

	rawConf, err := ioutil.ReadFile(filePath) // #nosec
	if err != nil {
		log.Printf("failed to read oauth config: %+v", err)
		return nil
	}

	conf := &oauth2.Config{}

	if err := json.Unmarshal(rawConf, conf); err != nil {
		log.Printf("failed to unmarshal oauth config: %+v", err)
		return nil
	}

	// HACK: remove once ccpgames/sso-issues#41 is done
	// provider := ctx.Value(Provider).(*oidc.Provider)
	// conf.Endpoint = provider.Endpoint()

	return conf
}

// NewOptions returns a new Options struct from cmd line flags
func NewOptions(ctx context.Context) context.Context {
	port := flag.Int("port", 8080, "backend port number")
	user := flag.String("db-user", "esi-isk", "db user name")
	host := flag.String("db-host", "postgres", "db host name")
	passwd := flag.String("db-passwd", "default", "db user password")
	name := flag.String("db-name", "esi-isk", "db name")
	sslmode := flag.String("ssl-mode", "disable", "db ssl mode option")
	debug := flag.Bool("debug", false, "enable debug mode")
	hostname := flag.String("hostname", "localhost", "hostname exposed as")
	https := flag.Bool("https", false, "should be addressed via https")
	production := flag.Bool("production", false, "if this is being run in prod")
	authConf := flag.String("auth", "/secret/sso.json", "path to auth config")
	esi := flag.String("esi", "https://esi.evetech.net", "basepath for ESI")
	characterID := flag.Int("character", 2114454465, "standings char ID")
	cacheTime := flag.Int("cache-time", 300, "seconds to cache responses for")
	cacheResp := flag.Int("cache-resp", 10000, "number of responses to cache")
	appSecret := flag.String("app-secret", "not-secure", "app secret to use")
	maxPrefLen := flag.Int("max-pref", 1500, "max length header/footer strings")
	maxPatternLen := flag.Int("max-pattern", 500, "max length row pattern string")
	maxPrefRows := flag.Int("max-rows", 100, "max number of rows to allow")

	flag.Parse()

	// HACK: remove once ccpgames/sso-issues#41 is done
	// provider := ctx.Value(Provider).(*oidc.Provider)

	opts := &Options{
		Production:  *production,
		Debug:       *debug,
		HTTPS:       *https,
		Hostname:    *hostname,
		Port:        *port,
		CharacterID: int32(*characterID),
		CacheTime:   *cacheTime,
		CacheResp:   *cacheResp,
		ESI:         *esi,
		DB: &DBOptions{
			Host:     *host,
			User:     *user,
			Password: *passwd,
			Name:     *name,
			Mode:     *sslmode,
		},
		Auth:          readAuthConf(ctx, *authConf),
		AppSecret:     *appSecret,
		MaxPrefLen:    int32(*maxPrefLen),
		MaxPatternLen: int32(*maxPatternLen),
		MaxPrefRows:   *maxPrefRows,
	}

	// HACK: remove once ccpgames/sso-issues#41 is done
	// ctx = context.WithValue(
	// 	ctx,
	// 	Verifier,
	// 	provider.Verifier(&oidc.Config{ClientID: opts.Auth.ClientID}),
	// )

	ctx = context.WithValue(ctx, Opts, opts)

	// HACK TEMPORARY UNTIL ccpgames/sso-issues#41
	ctx = context.WithValue(ctx, SSOClient, &http.Client{})

	return ctx
}
