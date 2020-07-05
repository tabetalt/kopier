package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/otiai10/copy"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Repositories []string `yaml:"repositories"`
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

	config := Config{}

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

func updateRepository(repo string, branchName string, auth http.BasicAuth, wg *sync.WaitGroup) {
	defer wg.Done()
	repoDir, err := ioutil.TempDir("", strings.ReplaceAll(repo, "/", "-"))

	if err != nil {
		panic(err)
	}

	r, err := git.PlainClone(repoDir, false, &git.CloneOptions{
		URL:      "https://github.com/" + repo + ".git",
		Auth:     &auth,
		Progress: os.Stdout,
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

	copy.Copy("./templates", repoDir)

	err = w.AddGlob(".")
	if err != nil {
		panic(err)
	}

	_, err = w.Commit("ci: Update devops-build-tools", &git.CommitOptions{
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
}

func createPullRequest(repo string, branchName string, headBranch string, auth http.BasicAuth) error {
	r := strings.Split(repo, "/")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: auth.Password},
	)

	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	title := "Update DevOps tools"
	body := `Hello 👋 We have some updates from [devops-build-tools](https://github.com/tabetalt/devops-build-tools) for you!`

	_, _, err := client.PullRequests.Create(ctx, r[0], r[1], &github.NewPullRequest{
		Title: &title,
		Head:  &branchName,
		Base:  &headBranch,
		Body:  &body,
	})

	return err
}
