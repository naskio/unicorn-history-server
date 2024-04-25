package repository

import (
	"context"
	"strings"

	"richscott/yhs/internal/event-collector/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"
)

type RepoPostgres struct {
	config *config.ECConfig
	dbpool *pgxpool.Pool
}

func NewECRepo(cfg *config.ECConfig) (error, *RepoPostgres) {
	poolCfg, err := pgxpool.ParseConfig(CreateConnectionString(cfg.PostgresConfig.Connection))
	if err != nil {
		return errors.Wrap(err, "cannot parse Postgres connection config"), nil
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return errors.Wrap(err, "cannot create Postgres connection pool"), nil
	}

	return nil, &RepoPostgres{config: cfg, dbpool: pool}
}

// Set up the DB for use, create tables
func (s *RepoPostgres) Setup(ctx context.Context) {
	setupStmts := []string{
		// For yunikorn-core/pkg/webservice/dao/ApplicationDAOInfo struct
		`DROP TABLE IF EXISTS applications`,
		`CREATE TABLE applications(
			id UUID,
			used_resource JSONB NOT NULL,
			max_used_resource JSONB NOT NULL,
			pending_resource JSONB NOT NULL,
			partition TEXT NOT NULL,
			queue_name TEXT NOT NULL,
			submission_time BIGINT NOT NULL,
			finished_time BIGINT,
			requests JSONB NOT NULL,
			allocations JSONB NOT NULL,
			state TEXT,
			"user" TEXT,
			groups TEXT[],
			rejected_message TEXT,
			state_log JSONB NOT NULL,
			place_holder_data JSONB NOT NULL,
			has_reserved BOOLEAN,
			reservations TEXT[],
			max_request_priority INTEGER,
			UNIQUE (id),
			PRIMARY KEY (id))`,
		// for yunikorn-core/pkg/webservice/dao/PartitionQueueDAOInfo struct
		`DROP TABLE IF EXISTS queues`,
		`CREATE TABLE queues(
			id UUID,
			queue_name TEXT NOT NULL,
			status TEXT,
			partition TEXT NOT NULL,
			pending_resource JSONB,
			max_resource JSONB,
			guaranteed_resource JSONB NOT NULL,
			allocated_resource JSONB NOT NULL,
			preempting_resource JSONB NOT NULL,
			head_room JSONB,
			is_leaf BOOLEAN,
			is_managed BOOLEAN,
			properties JSONB,
			parent TEXT,
			template_info JSONB,
			children UUID[],
			children_names TEXT[],
			abs_used_capacity JSONB,
			max_running_apps INTEGER,
			running_apps INTEGER NOT NULL,
			current_priority INTEGER,
			allocating_accepted_apps TEXT[],
			UNIQUE (id),
			PRIMARY KEY (id))`,
		// for yunikorn-core/pkg/webservice/dao/ClusterDAOInfo struct
		`DROP TABLE IF EXISTS clusters`,
		`CREATE TABLE clusters(
			id UUID,
			start_time BIGINT NOT NULL,
			rm_build_information JSONB,
			partition_name TEXT NOT NULL,
			cluster_name TEXT NOT NULL,
			UNIQUE (id),
			PRIMARY KEY (id))`,
		// for yunikorn-core/pkg/webservice/dao/NodeDAOInfo struct
		`DROP TABLE IF EXISTS nodes`,
		`CREATE TABLE nodes(
			id UUID,
			node_id TEXT NOT NULL,
			host_name TEXT NOT NULL,
			rack_name TEXT,
			attributes JSONB,
			capacity JSONB NOT NULL,
			allocated JSONB NOT NULL,
			occupied JSONB NOT NULL,
			available JSONB NOT NULL,
			utilized JSONB NOT NULL,
			allocations JSONB,
			schedulable BOOLEAN,
			is_reserved BOOLEAN,
			reservations TEXT[],
			UNIQUE (id),
			PRIMARY KEY (id))`,
	}

	for _, stmt := range setupStmts {
		_, err := s.dbpool.Exec(ctx, stmt)
		if err != nil {
			panic(err)
		}
	}
}

func CreateConnectionString(values map[string]string) string {
	pairs := []string{}

	replacer := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	for k, v := range values {
		pairs = append(pairs, k+"='"+replacer.Replace(v)+"'")
	}

	return strings.Join(pairs, " ")
}
