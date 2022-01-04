package cache

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"io"
	"os/exec"
	"path"

	"github.com/thought-machine/please/src/core"
	"github.com/thought-machine/please/src/fs"
)

type cmdCache struct {
	storeCommand    string
	retrieveCommand string
}

func keyToString(key []byte) string {
	return hex.EncodeToString(key)
}

func (cache *cmdCache) Store(target *core.BuildTarget, key []byte, files []string) {
	if cache.storeCommand != "" {
		strKey := keyToString(key)
		log.Debug("Storing %s: %s in custom cache...", target.Label, strKey)
		cmd := exec.Command("sh", "-c", cache.storeCommand)
		cmd.Env = append(cmd.Env, "CACHE_KEY="+strKey)

		r, w := io.Pipe()
		cmd.Stdin = r

		go cache.write(w, target, files)
		output, err := cmd.CombinedOutput()

		if err != nil {
			log.Debug("Failed to store files via custom command: %s", err)
		}

		if len(output) > 0 {
			log.Info("Custom command output:%s", string(output))
		}
	}
}

func (cache *cmdCache) Retrieve(target *core.BuildTarget, key []byte, _ []string) bool {

	strKey := keyToString(key)
	log.Debug("Retrieve %s: %s from custom cache...", target.Label, strKey)

	cmd := exec.Command("sh", "-c", cache.retrieveCommand)
	cmd.Env = append(cmd.Env, "CACHE_KEY="+strKey)

	var output bytes.Buffer
	cmd.Stderr = &output

	r, w := io.Pipe()
	cmd.Stdout = w

	if err := cmd.Start(); err != nil {
		log.Debug("Unable to start custom retrieve command: %s", err)
		return false
	}

	cmdResult := make(chan bool)

	go func() {
		var ok bool

		if err := cmd.Wait(); err != nil {
			log.Debug("Unable to unpack tar from custom command: %s", err)
			ok = false
		} else {
			ok = true
		}

		if output.Len() > 0 {
			log.Debug("Custom command output:%s", string(output.Bytes()))
		}
		// have to explicitely close the read here to potentially interrupt
		// a forever blocking tar reader in case that the command died
		// before even getting the first entry
		r.Close()

		cmdResult <- ok
	}()

	tarOk, err := readTar(r)
	if err != nil {
		log.Debug("Error in tar reader: %s", err)
	}

	return tarOk && <-cmdResult
}

func (cache *cmdCache) Clean(*core.BuildTarget) {
}

func (cache *cmdCache) CleanAll() {
}

func (cache *cmdCache) Shutdown() {
}

// write writes a series of files into the given Writer.
func (cache *cmdCache) write(w io.WriteCloser, target *core.BuildTarget, files []string) {
	defer w.Close()
	tw := tar.NewWriter(w)
	defer tw.Close()
	outDir := target.OutDir()

	for _, out := range files {
		if err := fs.Walk(path.Join(outDir, out), func(name string, isDir bool) error {
			return storeFile(tw, name)
		}); err != nil {
			log.Warning("Error sending artifacts to command-driven cache: %s", err)
		}
	}
}

func newCmdCache(config *core.Configuration) *cmdCache {
	return &cmdCache{
		storeCommand:    config.Cache.StoreCommand,
		retrieveCommand: config.Cache.RetrieveCommand,
	}
}
