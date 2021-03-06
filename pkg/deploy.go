package manager

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/barklan/pg-alembic-staging-mess/pkg/alembic"
	"github.com/barklan/pg-alembic-staging-mess/pkg/exec"
	"github.com/barklan/pg-alembic-staging-mess/pkg/reporting"
	"github.com/joho/godotenv"
)

var projectName = "nftg"

func resetAndDeployCmds(deployCmd string) []string {
	commands := []string{}

	pgTerminateConnection := `docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "SELECT pg_terminate_backend(pg_stat_activity.pid) FROM pg_stat_activity WHERE pg_stat_activity.datname = 'app' AND pid <> pg_backend_pid();"`
	pgTerminateConnectionMulti := []string{}
	for i := 0; i < 10; i++ {
		pgTerminateConnectionMulti = append(pgTerminateConnectionMulti, pgTerminateConnection)
	}

	commands = append(commands,
		`docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "REVOKE CONNECT ON DATABASE app FROM public;"`,
		`docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "SELECT pg_terminate_backend(pg_stat_activity.pid) FROM pg_stat_activity WHERE pg_stat_activity.datname = 'app' AND pid <> pg_backend_pid();"`,
	)
	commands = append(commands, pgTerminateConnectionMulti...)
	commands = append(commands,
		// Can be used with postgres 13
		// `docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "DROP DATABASE app WITH (FORCE);"`,
		`docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "DROP DATABASE IF EXISTS app;"`,
		`docker exec $(docker ps -q -f name=stag_db) bash -c "createdb -U postgres -T template0 app"`,
		`docker exec -i $(docker ps -q -f name=stag_db) psql -U postgres app < ../db_dump_stag.sql`,
		`docker exec $(docker ps -q -f name=stag_db) psql -U postgres -c "GRANT CONNECT ON DATABASE app TO public;"`,
		deployCmd,
	)

	return commands
}

func Deploy(target, backendImage string) {
	deployCmd := fmt.Sprintf(`cd /home/ubuntu/%s \
	&& docker login -u %s -p %s registry.gitlab.com/nftgalleryx/nftgallery_backend \
	&& docker stack deploy -c docker-stack.yml --with-registry-auth %s`,
		target,
		os.Getenv("GITLAB_TOKEN_USERNAME"),
		os.Getenv("GITLAB_TOKEN_PASSWORD"),
		target,
	)

	targetTag := os.Getenv("TAG")
	targetBranch := os.Getenv("BRANCH")

	targetAlembicHead := alembic.GetAlembicHeadFromImage(backendImage, targetTag)
	targetAlembicHistory := alembic.GetAlembicHistoryFromImage(backendImage, targetTag)
	currentAlembicVersion := alembic.GetAlembicVersionFromDB(target)

	currentAlembicVersionIsInHistory := false
	for _, val := range targetAlembicHistory {
		if currentAlembicVersion == val {
			currentAlembicVersionIsInHistory = true
			break
		}
	}

	var currentTag string
	err := godotenv.Load()
	if err != nil {
		currentTag = ""
		// currentBranch := ""
	} else {
		currentTag = os.Getenv("CURRENT_TAG")
		// currentBranch := os.Getenv("CURRENT_BRANCH")
	}

	commands := []string{deployCmd}
	// TODO make migrations part of ci instead of prestart script
	needMigrate := false

	var deployMsg string
	if targetTag == currentTag {
		deployMsg = "Fast deploy: deploying the same image."
	} else if target == "prod" {
		deployMsg = "Fast deploy: deploying on prod."
	} else if currentAlembicVersion == targetAlembicHead {
		deployMsg = "Fast deploy: alembic head is the same."
	} else if currentAlembicVersionIsInHistory {
		deployMsg = "Fast deploy: current alembic version exists in history."
		needMigrate = true
	} else {
		deployMsg = "Destructive deploy. Some data may be lost."
		commands = resetAndDeployCmds(deployCmd)
		needMigrate = true
	}
	log.Println(deployMsg)

	reportString := fmt.Sprintf("%s. %s Target branch: %s.", target, deployMsg, targetBranch)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	reporting.GoReport(wg, reportString)

	start := time.Now()
	exec.ExecuteCmds(commands)
	timeTook := time.Since(start)

	wg.Add(1)
	reporting.GoReport(wg, fmt.Sprintf("%s. Deploy successful. Approximate downtime: %s", target, timeTook))

	WriteCurrentVersion(targetTag, targetBranch)

	if needMigrate {
		log.Println("Launching migrations! (not really)")
		Migrate(target)
	}

	wg.Wait()
}
