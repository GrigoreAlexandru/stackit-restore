package stackit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	postgresflex "github.com/stackitcloud/stackit-sdk-go/services/postgresflex/v3api"
	"github.com/stackitcloud/stackit-sdk-go/services/postgresflex/v3api/wait"
)

var ErrDeleteInstanceForbidden = errors.New("instance could not be deleted due to permissions (403 Forbidden)")

func IsDeleteForbidden(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDeleteInstanceForbidden) {
		return true
	}
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) && oapiErr.StatusCode == 403 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "403") || strings.Contains(msg, "forbidden")
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) && oapiErr.StatusCode == 404 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

type Client struct {
	api          *postgresflex.APIClient
	projectID    string
	region       string
	outputWriter io.Writer
}

func (c *Client) SetOutputWriter(w io.Writer) {
	c.outputWriter = w
}

func (c *Client) logf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if c.outputWriter != nil {
		fmt.Fprint(c.outputWriter, msg)
	} else {
		fmt.Print(msg)
	}
}

type Instance struct {
	Name string
	ID   string
}

type Backup struct {
	Name      string
	CreatedAt time.Time
	Size      int64
}

type Database struct {
	Name  string
	ID    int64
	Owner string
}

func NewClient(cfg config.Config) (*Client, error) {
	opts := []sdkconfig.ConfigurationOption{
		sdkconfig.WithServiceAccountKeyPath(cfg.ServiceAccountKeyPath),
	}

	c, err := postgresflex.NewAPIClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create STACKIT postgresflex client: %w", err)
	}

	return &Client{
		api:       c,
		projectID: cfg.ProjectID,
		region:    cfg.Region,
	}, nil
}

func (c *Client) Region() string {
	return c.region
}

func (c *Client) ProjectID() string {
	return c.projectID
}

func (c *Client) GetInstances(ctx context.Context) ([]Instance, error) {
	list, err := c.api.DefaultAPI.ListInstances(ctx, c.projectID, c.region).Execute()
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0, len(list.Instances))
	for _, instance := range list.Instances {
		instances = append(instances, Instance{
			Name: instance.Name,
			ID:   instance.Id,
		})
	}

	return instances, nil
}

func (c *Client) GetBackups(ctx context.Context, instance Instance) ([]Backup, error) {
	list, err := c.api.DefaultAPI.ListBackups(ctx, c.projectID, c.region, instance.ID).Execute()
	if err != nil {
		return nil, err
	}

	backups := make([]Backup, 0, len(list.Backups))
	for _, backup := range list.Backups {
		createdAt, err := time.Parse(time.RFC3339, backup.CompletionTime)
		if err != nil {
			return nil, fmt.Errorf("parse backup %q completion time: %w", backup.Name, err)
		}

		backups = append(backups, Backup{
			Name:      backup.Name,
			CreatedAt: createdAt,
			Size:      backup.Size,
		})
	}

	return backups, nil
}

func (c *Client) GetDatabases(ctx context.Context, instance Instance) ([]Database, error) {
	list, err := c.api.DefaultAPI.ListDatabases(ctx, c.projectID, c.region, instance.ID).Execute()
	if err != nil {
		return nil, err
	}

	databases := make([]Database, 0, len(list.Databases))
	for _, database := range list.Databases {
		databases = append(databases, Database{
			Name:  database.Name,
			ID:    database.Id,
			Owner: database.Owner,
		})
	}

	return databases, nil
}

func (c *Client) CreateClone(ctx context.Context, instance Instance, pit time.Time) (Instance, error) {
	instResp, err := c.api.DefaultAPI.GetInstance(ctx, c.projectID, c.region, instance.ID).Execute()
	if err != nil {
		return Instance{}, fmt.Errorf("get instance %q (ID: %s) for clone configuration: %w", instance.Name, instance.ID, err)
	}

	class := instResp.Storage.GetClass()
	instSize := instResp.Storage.GetSize()

	sizeGB := instSize
	backups, bErr := c.GetBackups(ctx, instance)
	if bErr == nil && len(backups) > 0 {
		var matchedBackup *Backup
		for _, b := range backups {
			if !b.CreatedAt.After(pit) {
				if matchedBackup == nil || b.CreatedAt.After(matchedBackup.CreatedAt) {
					bCopy := b
					matchedBackup = &bCopy
				}
			}
		}
		if matchedBackup == nil {
			matchedBackup = &backups[0]
		}
		if matchedBackup != nil && matchedBackup.Size > 0 {
			backupGB := (matchedBackup.Size + (1 << 30) - 1) / (1 << 30)
			if backupGB > sizeGB {
				sizeGB = backupGB
			}
		}
	}
	if sizeGB < instSize {
		sizeGB = instSize
	}
	if sizeGB < 10 {
		sizeGB = 10
	}

	cloneName := formatTemporaryCloneName(instance.Name)
	overrides := postgresflex.CloneInstanceOverrides{
		Name: &cloneName,
		Size: sizeGB,
	}
	if class != "" {
		overrides.Class = &class
	}

	payload := postgresflex.CloneInstancePayload{
		InstanceOverrides: overrides,
		PointInTime:       pit,
	}

	c.logf("Initiating STACKIT clone %q from instance %q (ID: %s) at %s (Size: %d GB, Class: %s)...\n",
		cloneName, instance.Name, instance.ID, pit.Format(time.RFC3339), sizeGB, class)
	clone, err := c.api.DefaultAPI.CloneInstance(ctx, c.projectID, c.region, instance.ID).CloneInstancePayload(payload).Execute()
	if err != nil {
		return Instance{}, fmt.Errorf("create clone instance name %q: %w", instance.Name, err)
	}

	c.logf("Clone requested (ID: %s). Waiting for instance provisioning...\n", clone.Id)
	response, err := wait.CloneInstanceWaitHandler(ctx, c.api.DefaultAPI, c.projectID, c.region, clone.Id).WaitWithContext(ctx)
	if err != nil {
		// Clean up broken/partial instance so cloud state is not left dangling
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		_ = c.DeleteInstance(cleanupCtx, Instance{Name: cloneName, ID: clone.Id})
		return Instance{}, fmt.Errorf("wait for clone instance name %q, clone id %q: %w", instance.Name, clone.Id, err)
	}
	c.logf("Clone instance %q (ID: %s) is now ready.\n", response.Name, response.Id)

	cloneInst := Instance{
		Name: response.Name,
		ID:   response.Id,
	}

	host, port, endpointErr := c.GetInstanceEndpoint(ctx, cloneInst)
	if endpointErr == nil && host != "" {
		if waitErr := c.WaitForEndpointReady(ctx, host, port); waitErr != nil {
			c.logf("Warning: DNS/connectivity readiness check: %v. Proceeding...\n", waitErr)
		}
	}

	return cloneInst, nil
}

func (c *Client) WaitForEndpointReady(ctx context.Context, host string, port int32) error {
	c.logf("Waiting for DNS resolution and network connectivity on %s:%d...\n", host, port)
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for {
		// First verify DNS resolves
		_, dnsErr := net.LookupHost(host)
		if dnsErr == nil {
			// DNS resolved! Verify TCP connection
			conn, tcpErr := net.DialTimeout("tcp", target, 3*time.Second)
			if tcpErr == nil {
				conn.Close()
				c.logf("Connection established to %s:%d. Endpoint is ready.\n", host, port)
				return nil
			}
		}

		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out waiting for endpoint %s:%d to become reachable: %w", host, port, timeoutCtx.Err())
		case <-ticker.C:
		}
	}
}

func (c *Client) DeleteInstance(ctx context.Context, instance Instance) error {
	c.logf("Initiating deletion for temporary instance %q (ID: %s)...\n", instance.Name, instance.ID)
	err := c.api.DefaultAPI.DeleteInstance(ctx, c.projectID, c.region, instance.ID).Execute()
	if err != nil {
		if IsNotFound(err) {
			c.logf("Temporary instance %q (ID: %s) is already deleted or not found.\n", instance.Name, instance.ID)
			return nil
		}
		if IsDeleteForbidden(err) {
			c.logf("Warning: Temporary instance %q (ID: %s) could not be deleted due to permissions (403 Forbidden). Please delete it manually in STACKIT portal.\n", instance.Name, instance.ID)
			return ErrDeleteInstanceForbidden
		}
		return fmt.Errorf("delete instance %q: %w", instance.Name, err)
	}

	c.logf("Waiting for temporary instance %q deletion to complete...\n", instance.Name)
	_, err = wait.DeleteInstanceWaitHandler(ctx, c.api.DefaultAPI, c.projectID, c.region, instance.ID).WaitWithContext(ctx)
	if err != nil {
		if IsNotFound(err) {
			c.logf("Temporary instance %q deletion confirmed (resource removed).\n", instance.Name)
			return nil
		}
		if IsDeleteForbidden(err) {
			c.logf("Warning: Temporary instance %q (ID: %s) deletion check failed due to permissions (403 Forbidden).\n", instance.Name, instance.ID)
			return ErrDeleteInstanceForbidden
		}
		return fmt.Errorf("wait for deletion of instance %q: %w", instance.Name, err)
	}
	c.logf("Temporary instance %q successfully deleted.\n", instance.Name)

	return nil
}

func (c *Client) GetInstanceEndpoint(ctx context.Context, instance Instance) (string, int32, error) {
	response, err := c.api.DefaultAPI.GetInstance(ctx, c.projectID, c.region, instance.ID).Execute()
	if err != nil {
		return "", 0, fmt.Errorf("get instance details %q: %w", instance.Name, err)
	}

	host := response.ConnectionInfo.Write.Host
	port := response.ConnectionInfo.Write.Port
	if host == "" {
		return "", 0, fmt.Errorf("instance %q (ID: %s) did not return host in connection info", instance.Name, instance.ID)
	}
	if port == 0 {
		port = 5432
	}
	return host, port, nil
}

func formatTemporaryCloneName(sourceName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(sourceName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		clean = "source"
	}
	// Max length for STACKIT instance name is 63 chars.
	// Suffix "-temp-clone-<timestamp>" is ~23 chars, so limit clean name to 38 chars.
	if len(clean) > 38 {
		clean = strings.TrimRight(clean[:38], "-")
	}
	return fmt.Sprintf("%s-temp-clone-%d", clean, time.Now().Unix())
}

