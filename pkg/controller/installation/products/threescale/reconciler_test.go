package threescale

import (
	"context"
	threescalev1 "github.com/integr8ly/integreatly-operator/pkg/apis/3scale/v1alpha1"
	aerogearv1 "github.com/integr8ly/integreatly-operator/pkg/apis/aerogear/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	integreatlyv1alpha1 "github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/products/rhsso"
	coreosv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1"
	coreosv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	marketplacev1 "github.com/operator-framework/operator-marketplace/pkg/apis/operators/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	pkgclient "sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func getBuildScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	err := threescalev1.SchemeBuilder.AddToScheme(scheme)
	err = aerogearv1.SchemeBuilder.AddToScheme(scheme)
	err = integreatlyv1alpha1.SchemeBuilder.AddToScheme(scheme)
	err = operatorsv1alpha1.AddToScheme(scheme)
	err = marketplacev1.SchemeBuilder.AddToScheme(scheme)
	err = corev1.SchemeBuilder.AddToScheme(scheme)
	err = coreosv1.SchemeBuilder.AddToScheme(scheme)
	return scheme, err
}

func TestThreeScale(t *testing.T) {
	t.Run("Test successful 3scale reconcile", func(*testing.T) {

		integreatlyOperatorNamespace := "integreatly-operator-namespace"
		clusterPreReqObjects, appsv1PreReqObjects := GetClusterPreReqObjects(integreatlyOperatorNamespace, defaultInstallationNamespace)
		scheme, err := getBuildScheme()
		if err != nil {
			t.Fatalf("Error getting pre req objects for %s: %v", packageName, err)
		}
		configManager, fakeSigsClient, fakeAppsV1Client, fakeOauthClient, fakeThreeScaleClient, mpm, err := GetClients(clusterPreReqObjects, scheme, appsv1PreReqObjects)
		if err != nil {
			t.Fatalf("Error creating clients for %s: %v", packageName, err)
		}

		// Create Installation and reconcile on it.
		installation := &v1alpha1.Installation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-installation",
				Namespace: integreatlyOperatorNamespace,
			},
			Spec: v1alpha1.InstallationSpec{
				MasterURL:        "https://console.apps.example.com",
				RoutingSubdomain: "apps.example.com",
			},
		}
		ctx := context.TODO()
		testReconciler, err := NewReconciler(configManager, installation, fakeAppsV1Client, fakeOauthClient, fakeThreeScaleClient, mpm)
		status, err := testReconciler.Reconcile(ctx, installation, fakeSigsClient)
		if err != nil {
			t.Fatalf("Error reconciling %s: %v", packageName, err)
		}

		if status != v1alpha1.PhaseCompleted {
			t.Fatalf("unexpected status: %v, expected: %v", status, v1alpha1.PhaseCompleted)
		}

		// A namespace should have been created.
		ns := &corev1.Namespace{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: defaultInstallationNamespace}, ns)
		if k8serr.IsNotFound(err) {
			t.Fatalf("%s namespace was not created", packageName)
		}

		// A subscription to the product operator should have been created.
		sub := &coreosv1alpha1.Subscription{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: packageName, Namespace: defaultInstallationNamespace}, sub)
		if k8serr.IsNotFound(err) {
			t.Fatalf("%s operator subscription was not created", packageName)
		}

		// The main s3credentials should have been copied into the 3scale namespace.
		s3Credentials := &corev1.Secret{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: s3CredentialsSecretName, Namespace: defaultInstallationNamespace}, s3Credentials)
		if k8serr.IsNotFound(err) {
			t.Fatalf("s3Credentials were not copied into %s namespace", defaultInstallationNamespace)
		}

		// The product custom resource should have been created.
		apim := &threescalev1.APIManager{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: apiManagerName, Namespace: defaultInstallationNamespace}, apim)
		if k8serr.IsNotFound(err) {
			t.Fatalf("APIManager '%s' was not created", apiManagerName)
		}
		if apim.Spec.WildcardDomain != installation.Spec.RoutingSubdomain {
			t.Fatalf("APIManager wildCardDomain is misconfigured. '%s' should be '%s'", apim.Spec.WildcardDomain, installation.Spec.RoutingSubdomain)
		}

		// RHSSO integration should be configured.
		kcr := &aerogearv1.KeycloakRealm{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: rhsso.KeycloakRealmName, Namespace: rhsso.DefaultRhssoNamespace}, kcr)
		if !containsClient(kcr.Spec.Clients, clientId) {
			t.Fatalf("Keycloak client '%s' was not created", clientId)
		}
		integrationCall := fakeThreeScaleClient.AddSSOIntegrationCalls()[0]
		if integrationCall.Data["client_id"] != clientId || integrationCall.Data["site"] != string(rhssoCredentialsSecret.Data["SSO_ADMIN_URL"])+keycloakRealmPath {
			t.Fatalf("SSO integration request to 3scale API was incorrect")
		}

		// RHSSO admin user should be set as 3scale admin
		updateAdminCall := fakeThreeScaleClient.UpdateAdminPortalUserDetailsCalls()[0]
		if updateAdminCall.Username != rhsso.CustomerAdminUser.UserName || updateAdminCall.Email != rhsso.CustomerAdminUser.Email {
			t.Fatalf("Request to 3scale API to update admin details was incorrect")
		}
		adminSecret := &corev1.Secret{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: threeScaleAdminDetailsSecret.Name, Namespace: defaultInstallationNamespace}, adminSecret)
		if string(adminSecret.Data["ADMIN_USER"]) != rhsso.CustomerAdminUser.UserName || string(adminSecret.Data["ADMIN_EMAIL"]) != rhsso.CustomerAdminUser.Email {
			t.Fatalf("3scale admin secret details were not updated")
		}

		// Service discovery should be configured
		threeScaleOauth, err := fakeOauthClient.OAuthClients().Get(oauthId, metav1.GetOptions{})
		if k8serr.IsNotFound(err) {
			t.Fatalf("3scale should have an Ouath Client '%s' created", oauthId)
		}
		if threeScaleOauth.RedirectURIs[0] != installation.Spec.MasterURL {
			t.Fatalf("3scale Ouath Client redirect uri should be %s and is %s", installation.Spec.MasterURL, threeScaleOauth.RedirectURIs[0])
		}
		serviceDiscoveryConfigMap := &corev1.ConfigMap{}
		err = fakeSigsClient.Get(ctx, pkgclient.ObjectKey{Name: threeScaleServiceDiscoveryConfigMap.Name, Namespace: defaultInstallationNamespace}, serviceDiscoveryConfigMap)
		if string(adminSecret.Data["ADMIN_USER"]) != rhsso.CustomerAdminUser.UserName || string(adminSecret.Data["ADMIN_EMAIL"]) != rhsso.CustomerAdminUser.Email {
			t.Fatalf("3scale admin secret details were not updated")
		}
		if string(serviceDiscoveryConfigMap.Data["service_discovery.yml"]) != sdConfig {
			t.Fatalf("Service discovery config is misconfigured")
		}

		// system-app and system-sidekiq deploymentconfigs should have been rolled out
		sa, err := fakeAppsV1Client.DeploymentConfigs(defaultInstallationNamespace).Get("system-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Error getting deplymentconfig: %v", err)
		}
		if sa.Status.LatestVersion == 1 {
			t.Fatalf("system-app was not rolled out")
		}
		ssk, err := fakeAppsV1Client.DeploymentConfigs(defaultInstallationNamespace).Get("system-sidekiq", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Error getting deplymentconfig: %v", err)
		}
		if ssk.Status.LatestVersion == 1 {
			t.Fatalf("system-sidekiq was not rolled out")
		}
	})
}
