package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/otiai10/copy"
	"golang.org/x/oauth2"

	gt "github.com/sabhiram/go-gitignore"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/karrick/godirwalk"
	"gopkg.in/yaml.v2"
)

type config struct {
	Repositories []string `yaml:"repositories"`
}

type buildConfig struct {
	Ignore []string `yaml:"ignore"`
}

type repoConfig struct {
	Title       string      `yaml:"title"`
	DisplayName string      `yaml:"displayName"`
	ServiceName string      `yaml:"serviceName"`
	Description string      `yaml:"description"`
	Type        string      `yaml:"type"`
	Protocol    string      `yaml:"protocol"`
	Build       buildConfig `yaml:"build"`
}

func main() {
	// Get GITHUB_TOKEN
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("Missing GITHUB_TOKEN env")
	}

	auth := http.BasicAuth{
		Username: "x-access-token",
		Password: token,
	}

	config := config{}

	configBytes, err := ioutil.ReadFile("./config.yml")

	if err != nil {
		panic(err)
	}

	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		panic(err)
	}

	timestamp := time.Now().Unix()
	branchName := fmt.Sprintf("devops-d-%d", timestamp)
	var wg sync.WaitGroup
	for _, repo := range config.Repositories {
		wg.Add(1)
		go updateRepository(repo, branchName, auth, &wg)
	}
	wg.Wait()
}

func generateTemplates(config *repoConfig, repo string, repoDir string) error {
	t := config.Type
	if t == "" {
		return errors.New("repository type not set")
	}

	// Determine what template folder to choose.
	tmplPath := fmt.Sprintf("templates/%s", config.Type)

	tempTmplDir, err := ioutil.TempDir("", "")

	if err != nil {
		return err
	}

	copy.Copy(tmplPath, tempTmplDir)

	err = godirwalk.Walk(tempTmplDir, &godirwalk.Options{
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			if de.IsDir() {
				return nil
			}

			t, err := template.New(de.Name()).ParseFiles(osPathname)
			if err != nil {
				return err
			}

			buf := bytes.NewBuffer([]byte(""))
			if err := t.Execute(buf, config); err != nil {
				fmt.Printf("[ERROR] Something wrong with template (%s).", osPathname)
				return err
			}

			if buf.Len() == 0 {
				fmt.Printf("[NOTICE] %s was not templated (%s).", de.Name(), osPathname)
				return nil
			}

			if err := ioutil.WriteFile(osPathname, buf.Bytes(), 0644); err != nil {
				return err
			}

			return nil
		},
		Unsorted: true,
	})

	if err != nil {
		return err
	}

	if len(config.Build.Ignore) > 0 {
		i, _ := gt.CompileIgnoreLines(config.Build.Ignore...)
		err = godirwalk.Walk(tempTmplDir, &godirwalk.Options{
			Callback: func(osPathname string, de *godirwalk.Dirent) error {
				out := strings.ReplaceAll(osPathname, tempTmplDir, repoDir)

				if !i.MatchesPath(osPathname) && de.IsRegular() {
					os.MkdirAll(filepath.Dir(osPathname), 0755)
					return copy.Copy(osPathname, out)
				}
				return nil
			},
			Unsorted: true,
		})
	} else {
		err = copy.Copy(tempTmplDir, repoDir)
	}

	if err != nil {
		return err
	}

	return os.RemoveAll(tempTmplDir)
}

func updateRepository(repo string, branchName string, auth http.BasicAuth, wg *sync.WaitGroup) error {
	defer wg.Done()
	repoDir, err := ioutil.TempDir("", strings.ReplaceAll(repo, "/", "-"))

	if err != nil {
		panic(err)
	}

	r, err := git.PlainClone(repoDir, false, &git.CloneOptions{
		URL:  "https://github.com/" + repo + ".git",
		Auth: &auth,
	})

	headRef, err := r.Head()
	if err != nil {
		panic(err)
	}

	w, err := r.Worktree()
	if err != nil {
		panic(err)
	}

	w.Checkout(&git.CheckoutOptions{
		Create: true,
		Branch: plumbing.NewBranchReferenceName(branchName),
	})

	rc, err := ioutil.ReadFile(repoDir + "/service-config.yaml")
	if err != nil {
		rc, err = ioutil.ReadFile(repoDir + "/service-config.yml")
	}
	if err != nil {
		panic(err)
	}

	var rcn repoConfig
	err = yaml.Unmarshal(rc, &rcn)
	if err != nil {
		panic(err)
	}

	err = generateTemplates(&rcn, repo, repoDir)

	if err != nil {
		panic(err)
	}

	status, err := w.Status()

	if len(status) == 0 {
		defer os.RemoveAll(repoDir)
		log.Printf("%s does not need an update", repo)
		return nil
	}

	err = w.AddGlob(".")
	if err != nil {
		panic(err)
	}

	_, err = w.Commit("ci: Update kopier", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Mr. Louie Paytron",
			Email: "paytron@tabetalt.no",
			When:  time.Now(),
		},
	})

	if err != nil {
		panic(err)
	}

	err = r.Push(&git.PushOptions{
		Auth: &auth,
	})

	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(repoDir)

	err = createPullRequest(repo, branchName, headRef.Name().Short(), auth)
	if err != nil {
		panic(err)
	}

	return nil
}

func createPullRequest(repo string, branchName string, headBranch string, auth http.BasicAuth) error {
	r := strings.Split(repo, "/")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: auth.Password},
	)

	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	title := "Update Kopier files"
	body := `Hello 👋 We have some updates from [kopier](https://github.com/tabetalt/kopier) for you!`

	_, _, err := client.PullRequests.Create(ctx, r[0], r[1], &github.NewPullRequest{
		Title: &title,
		Head:  &branchName,
		Base:  &headBranch,
		Body:  &body,
	})

	return err
}
