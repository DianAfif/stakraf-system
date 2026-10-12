package main

import (
	"context"
	"encoding/json"
	"regexp"
)

type ContainerReport struct {
	AppID  string `json:"app_id"`
	Status string `json:"status"`
}

var resourceIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func validContainerReports(reports []ContainerReport) bool {
	if len(reports) > 200 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range reports {
		if !resourceIDPattern.MatchString(item.AppID) || !oneOf(item.Status, "running", "stopped", "restarting", "created", "dead", "removing") || seen[item.AppID] {
			return false
		}
		seen[item.AppID] = true
	}
	return true
}
func (a *API) reconcileContainers(ctx context.Context, sid, tokenHash string, reports []ContainerReport) error {
	if reports == nil {
		reports = []ContainerReport{}
	}
	data, err := json.Marshal(reports)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `WITH inventory AS (
	 SELECT * FROM jsonb_to_recordset($2::jsonb) AS x(app_id text,status text)
	) UPDATE apps a SET status=CASE
	 WHEN i.status='running' THEN 'running'
	 WHEN i.status='stopped' THEN 'stopped'
	 WHEN i.status IS NULL THEN 'missing'
	 ELSE 'unknown' END,
	 last_error=CASE WHEN i.status IS NULL THEN 'Managed container was not found on this host.' WHEN i.status IN ('running','stopped') THEN '' ELSE 'Container is '||i.status END
	 FROM (SELECT ap.id,iv.status FROM apps ap LEFT JOIN inventory iv ON iv.app_id=ap.id WHERE ap.server_id=$1) i
	 WHERE a.id=i.id AND a.status!='queued'
	 AND EXISTS(SELECT 1 FROM servers WHERE id=$1 AND agent_token_hash=$3)
	 AND NOT EXISTS(SELECT 1 FROM jobs WHERE app_id=a.id AND status IN ('queued','running') AND kind!='logs')`, sid, data, tokenHash)
	return err
}
