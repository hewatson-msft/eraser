//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/kind/pkg/cluster"
)

type Name string

const depTest Name = "dep-test"

var (
	podSelectorLabels = map[string]string{"app": "nginx"}
)

func TestRemoveImagesFromAllNodes(t *testing.T) {
	depFeat := features.New("Test Remove Images From All Nodes").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {

			dep := newDeployment(cfg.Namespace(), "dep-test", 2, podSelectorLabels)
			if err := cfg.Client().Resources().Create(ctx, dep); err != nil {
				t.Error("Failed to create the dep", err)
			}

			return ctx
		}).
		Assess("deployment successfully deployed", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client, err := cfg.NewClient()
			if err != nil {
				t.Error("Failed to create new client", err)
			}

			resultDeployment := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "dep-test", Namespace: cfg.Namespace()},
			}

			if err = wait.For(conditions.New(client.Resources()).DeploymentConditionMatch(&resultDeployment, appsv1.DeploymentAvailable, corev1.ConditionTrue),
				wait.WithTimeout(time.Minute*1)); err != nil {
				t.Error("deployment not found", err)
			}

			return context.WithValue(ctx, depTest, &resultDeployment)
		}).
		Assess("Pods successfully deployed to all nodes", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// get Kind nodeList
			provider := cluster.NewProvider(cluster.ProviderWithDocker())

			nodeList, err := provider.ListNodes(kindClusterName)
			if err != nil {
				t.Error("Cannot list Kind node list", err)
			}

			// get podList
			podList := &corev1.PodList{}
			if err := cfg.Client().Resources(cfg.Namespace()).List(ctx, podList); err != nil {
				t.Error("Cannot get pod list", err)
			}

			if len(podList.Items) == 0 {
				t.Errorf("no pods in namespace %s", cfg.Namespace())
			}

			for _, node := range nodeList {
				podFound := false
				// make sure pod is running on each node
				for _, pod := range podList.Items {
					if (pod.Spec.NodeName == node.String() && !podFound) || strings.Contains(node.String(), "control-plane") {
						podFound = true
					}
				}
				if !podFound {
					t.Errorf("pod is not running in the node %s", node.String())
				}
			}
			return ctx
		}).
		Assess("Images successfully deleted from all nodes", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			//delete deployment
			client, err := cfg.NewClient()
			if err != nil {
				t.Error("Failed to create new client", err)
			}
			dep := ctx.Value(depTest).(*appsv1.Deployment)
			if err := client.Resources().Delete(ctx, dep); err != nil {
				t.Error("Failed to delete the dep", err)
			}

			// deploy imageJob config
			if err := deployEraserConfig(cfg.KubeconfigFile(), "eraser-system", "test-data", "eraser_v1alpha1_imagelist.yaml"); err != nil {
				t.Error("Failed to deploy image list config", err)
			}

			//get image list and make sure the targeted image not there

			// get Kind nodeList
			provider := cluster.NewProvider(cluster.ProviderWithDocker())

			nodeList, err := provider.ListNodes(kindClusterName)
			if err != nil {
				t.Error("Cannot list Kind node list", err)
			}

			time.Sleep(1 * time.Minute)

			for _, node := range nodeList {
				if strings.Contains(node.String(), "control-plane") {
					continue
				}
				images, err := DockerExec(node.String())
				if err != nil {
					t.Error("Cannot list images", err)
				}
				if strings.Contains(images, "nginx") || strings.Contains(images, "docker.io/library/nginx:latest") {
					t.Error("Image found after running imagelist")
				}
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			//remove imagelist
			if err := deleteEraserConfig(cfg.KubeconfigFile(), "eraser-system", "test-data", "eraser_v1alpha1_imagelist.yaml"); err != nil {
				t.Error("Failed to delete image list config ", err)
			}
			//remove imagejob(s)
			if err := KubectlDelete(cfg.KubeconfigFile(), "eraser-system", append([]string{"imagejob", "--all"})); err != nil {
				t.Error("Failed to delete image job(s) config ", err)
			}
			return ctx
		}).Feature()

	testenv.Test(t, depFeat)
}