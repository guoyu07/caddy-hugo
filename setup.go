package hugo

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/hacdias/caddy-filemanager"
	"github.com/hacdias/caddy-filemanager/config"
	"github.com/hacdias/caddy-filemanager/directory"
	"github.com/hacdias/caddy-filemanager/frontmatter"
	"github.com/hacdias/caddy-hugo/utils/commands"
	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyhttp/httpserver"
)

// AssetsURL is the base url for the assets
const (
	AssetsURL    = "/_hugointernal"
	HugoNotFound = "It seems that you don't have 'hugo' on your PATH."
)

func init() {
	caddy.RegisterPlugin("hugo", caddy.Plugin{
		ServerType: "http",
		Action:     setup,
	})
}

// Setup is the init function of Caddy plugins and it configures the whole
// middleware thing.
func setup(c *caddy.Controller) error {
	cnf := httpserver.GetConfig(c)

	conf, fm, err := parse(c, cnf.Root)

	if err != nil {
		return err
	}

	// Generates the Hugo website for the first time the plugin is activated.
	go RunHugo(conf, true)

	mid := func(next httpserver.Handler) httpserver.Handler {
		fm.Next = next

		return &Hugo{
			Next:        next,
			Config:      conf,
			FileManager: fm,
		}
	}

	cnf.AddMiddleware(mid)
	return nil
}

// Config is a configuration for managing a particular hugo website.
type Config struct {
	Public  string   // Public content path
	Root    string   // Hugo files path
	Hugo    string   // Hugo executable location
	Styles  string   // Admin styles path
	Args    []string // Hugo arguments
	BaseURL string   // BaseURL of admin interface
}

// Parse parses the configuration set by the user so it can be
// used by the middleware
func parse(c *caddy.Controller, root string) (*Config, *filemanager.FileManager, error) {
	var (
		cfg    *Config
		fm     *filemanager.FileManager
		err    error
		tokens string
	)

	cfg = new(Config)

	if cfg.Hugo, err = exec.LookPath("hugo"); err != nil {
		fmt.Println(HugoNotFound)
		return cfg, fm, errors.New(HugoNotFound)
	}

	for c.Next() {
		cfg.Public = strings.Replace(root, "./", "", -1)
		cfg.BaseURL = "/admin"
		cfg.Root = "./"

		args := c.RemainingArgs()

		if len(args) >= 1 {
			cfg.Root = args[0]
			cfg.Root = strings.TrimSuffix(cfg.Root, "/")
			cfg.Root += "/"
		}

		if len(args) >= 2 {
			cfg.BaseURL = args[1]
			cfg.BaseURL = strings.TrimPrefix(cfg.BaseURL, "/")
			cfg.BaseURL = "/" + cfg.BaseURL
		}

		for c.NextBlock() {
			switch c.Val() {
			case "flag":
				if !c.NextArg() {
					return cfg, &filemanager.FileManager{}, c.ArgErr()
				}

				values := strings.Split(c.Val(), " ")

				if len(values) == 0 {
					return cfg, fm, errors.New("Not enough arguments for 'flag' option.")
				}

				value := "true"

				if len(values) > 1 {
					value = values[1]
				}

				cfg.Args = append(cfg.Args, "--"+values[0]+"="+value)
			default:
				line := "\n\t" + c.Val()

				if c.NextArg() {
					line += " " + c.Val()
				}

				tokens += line
			}
		}
	}

	tokens = "filemanager " + cfg.BaseURL + " {\n\tshow " + cfg.Root + tokens
	tokens += "\n}"

	fmConfig, err := config.Parse(caddy.NewTestController("http", tokens))

	if err != nil {
		return cfg, fm, err
	}

	fm = &filemanager.FileManager{Configs: fmConfig}
	fm.Configs[0].HugoEnabled = true

	format := getFrontMatter(cfg)

	for _, user := range fm.Configs[0].Users {
		user.FrontMatter = format
	}

	if err != nil {
		return cfg, fm, err
	}

	return cfg, fm, nil
}

func getFrontMatter(conf *Config) string {
	format := "toml"

	// Checks if there is an Hugo website in the path that is provided.
	// If not, a new website will be created.
	create := true

	if _, err := os.Stat(conf.Root + "config.yaml"); err == nil {
		format = "yaml"
		create = false
	}

	if _, err := os.Stat(conf.Root + "config.json"); err == nil {
		format = "json"
		create = false
	}

	if _, err := os.Stat(conf.Root + "config.toml"); err == nil {
		format = "toml"
		create = false
	}

	if create {
		err := commands.Run(conf.Hugo, []string{"new", "site", conf.Root, "--force"}, ".")
		if err != nil {
			log.Fatal(err)
		}
	}

	// Get Default FrontMatter
	bytes, err := ioutil.ReadFile(filepath.Clean(conf.Root + "/config." + format))

	if err != nil {
		log.Println(err)
		fmt.Printf("Can't get the default frontmatter from the configuration. %s will be used.\n", format)
	} else {
		bytes = directory.AppendFrontMatterRune(bytes, format)
		f, err := frontmatter.Unmarshal(bytes)

		if err != nil {
			log.Println(err)
			fmt.Printf("Can't get the default frontmatter from the configuration. %s will be used.\n", format)
		} else {
			kind := reflect.TypeOf(f)

			if kind == reflect.TypeOf(map[interface{}]interface{}{}) {
				if val, ok := f.(map[interface{}]interface{})["metaDataFormat"]; ok {
					format = val.(string)
				}

			} else {
				if val, ok := f.(map[string]interface{})["metaDataFormat"]; ok {
					format = val.(string)
				}
			}
		}
	}

	return format
}
