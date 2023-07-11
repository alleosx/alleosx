/*

   Copyright 2020 Docker Compose CLI authors
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/watch"
	"github.com/jonboulle/clockwork"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
)

func Test_debounce(t *testing.T) {
	ch := make(chan fileMapping)
	var (
		ran int
		got []string
	)
	clock := clockwork.NewFakeClock()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		debounce(ctx, clock, quietPeriod, ch, func(services rebuildServices) {
			for svc := range services {
				got = append(got, svc)
			}
			ran++
			stop()
		})
		return nil
	})
	for i := 0; i < 100; i++ {
		ch <- fileMapping{Service: "test"}
	}
	assert.Equal(t, ran, 0)
	clock.Advance(quietPeriod)
	err := eg.Wait()
	assert.NilError(t, err)
	assert.Equal(t, ran, 1)
	assert.DeepEqual(t, got, []string{"test"})
}

type testWatcher struct {
	events chan watch.FileEvent
	errors chan error
}

func (t testWatcher) Start() error {
	return nil
}

func (t testWatcher) Close() error {
	return nil
}

func (t testWatcher) Events() chan watch.FileEvent {
	return t.events
}

func (t testWatcher) Errors() chan error {
	return t.errors
}

func Test_sync(t *testing.T) {
	needSync := make(chan fileMapping)
	needRebuild := make(chan fileMapping)
	ctx, cancelFunc := context.WithCancel(context.TODO())
	defer cancelFunc()

	run := func() watch.Notify {
		watcher := testWatcher{
			events: make(chan watch.FileEvent, 1),
			errors: make(chan error),
		}

		go func() {
			cli, err := command.NewDockerCli()
			assert.NilError(t, err)

			service := composeService{
				dockerCli: cli,
			}
			err = service.watch(ctx, "test", watcher, []Trigger{
				{
					Path:   "/src",
					Action: "sync",
					Target: "/work",
					Ignore: []string{"ignore"},
				},
				{
					Path:   "/",
					Action: "rebuild",
				},
			}, needSync, needRebuild)
			assert.NilError(t, err)
		}()
		return watcher
	}

	t.Run("synchronize file", func(t *testing.T) {
		watcher := run()
		watcher.Events() <- watch.NewFileEvent("/src/changed")
		select {
		case actual := <-needSync:
			assert.DeepEqual(t, fileMapping{Service: "test", HostPath: "/src/changed", ContainerPath: "/work/changed"}, actual)
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout")
		}
	})

	t.Run("ignore", func(t *testing.T) {
		watcher := run()
		watcher.Events() <- watch.NewFileEvent("/src/ignore")
		select {
		case <-needSync:
			t.Error("file event should have been ignored")
		case <-time.After(100 * time.Millisecond):
			// expected
		}
	})

	t.Run("rebuild", func(t *testing.T) {
		watcher := run()
		watcher.Events() <- watch.NewFileEvent("/dependencies.yaml")
		select {
		case event := <-needRebuild:
			assert.Equal(t, "test", event.Service)
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout")
		}
	})

}

func TestLoadDevelopmentConfig_Symlinks(t *testing.T) {
	if testing.Short() {
		t.Skip("Test creates temporary files")
	}

	tmpdir := t.TempDir()
	projDir := filepath.Join(tmpdir, "project")
	assert.NilError(t, os.Mkdir(projDir, 0o700))
	aliasProjDir := filepath.Join(tmpdir, "proj-symlink")
	assert.NilError(t, os.Symlink(projDir, aliasProjDir))

	proj := types.Project{
		WorkingDir: aliasProjDir,
	}

	svc := types.ServiceConfig{
		Name: "app",
		Extensions: types.Extensions{
			"x-develop": map[string]interface{}{
				"watch": []map[string]interface{}{
					{
						"path":   ".",
						"action": "sync",
					},
				},
			},
		},
	}

	cfg, err := loadDevelopmentConfig(svc, &proj)
	assert.NilError(t, err)

	realProjDir, err := filepath.EvalSymlinks(projDir)
	assert.NilError(t, err)
	assert.DeepEqual(t, []Trigger{
		{
			Path:   realProjDir,
			Action: "sync",
		},
	}, cfg.Watch)
}
