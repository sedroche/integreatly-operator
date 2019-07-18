package threescale

import (
	chev1 "github.com/eclipse/che-operator/pkg/apis/org/v1"
	"github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/marketplace"
	operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"

	aerogearv1 "github.com/integr8ly/integreatly-operator/pkg/apis/aerogear/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/products/config"
	fakepkgclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	kafkav1 "github.com/integr8ly/integreatly-operator/pkg/apis/kafka.strimzi.io/v1alpha1"
	appsv1Client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	fakeappsv1Client "github.com/openshift/client-go/apps/clientset/versioned/fake"
	oauthClient "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	fakeoauthClient "github.com/openshift/client-go/oauth/clientset/versioned/fake"
	marketplacev1 "github.com/operator-framework/operator-marketplace/pkg/apis/operators/v1"
	corev1 "k8s.io/api/core/v1"
)

func basicConfigMock() *config.ConfigReadWriterMock {
	return &config.ConfigReadWriterMock{}
}

func buildScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	chev1.SchemeBuilder.AddToScheme(scheme)
	aerogearv1.SchemeBuilder.AddToScheme(scheme)
	v1alpha1.SchemeBuilder.AddToScheme(scheme)
	operatorsv1alpha1.AddToScheme(scheme)
	marketplacev1.SchemeBuilder.AddToScheme(scheme)
	kafkav1.SchemeBuilder.AddToScheme(scheme)
	corev1.SchemeBuilder.AddToScheme(scheme)
	return scheme
}

func TestCodeready(t *testing.T) {
	scenarios := []struct {
		Name                 string
		ExpectedStatus       v1alpha1.StatusPhase
		ExpectedError        string
		ExpectedCreateError  string
		Object               *v1alpha1.Installation
		FakeAppsV1Client     appsv1Client.AppsV1Interface
		FakeOauthClient      oauthClient.OauthV1Interface
		FakeConfig           *config.ConfigReadWriterMock
		FakeControllerClient client.Client
		FakeMPM              *marketplace.MarketplaceInterfaceMock
		ValidateCallCounts   func(mockConfig *config.ConfigReadWriterMock, mockMPM *marketplace.MarketplaceInterfaceMock, t *testing.T)
	}{
		{
			Name:                 "Test PhaseNone",
			ExpectedStatus:       v1alpha1.PhaseInProgress,
			Object:               &v1alpha1.Installation{},
			FakeAppsV1Client:     fakeappsv1Client.NewSimpleClientset(nil).AppsV1(),
			FakeOauthClient:      fakeoauthClient.NewSimpleClientset(nil).OauthV1(),
			FakeControllerClient: fakepkgclient.NewFakeClient(),
			FakeConfig:           basicConfigMock(),
		},
		//{
		//	Name:                 "test no phase with creatNamespaces",
		//	ExpectedStatus:       v1alpha1.PhaseAwaitingNS,
		//	Object:               &v1alpha1.Installation{},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClient(),
		//	FakeConfig:           basicConfigMock(),
		//},
		//{
		//	Name:           "test subscription phase",
		//	ExpectedStatus: v1alpha1.PhaseAwaitingOperator,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseCreatingSubscription),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeMPM: &marketplace.MarketplaceInterfaceMock{
		//		CreateSubscriptionFunc: func(serverClient pkgclient.Client, os operatorsv1.OperatorSource, ns string, pkg string, channel string, operatorGroupNamespaces []string, approvalStrategy operatorsv1alpha1.Approval) error {
		//			return nil
		//		},
		//	},
		//	FakeControllerClient: fakepkgclient.NewFakeClient(),
		//	FakeConfig:           basicConfigMock(),
		//	ValidateCallCounts: func(mockConfig *config.ConfigReadWriterMock, mockMPM *marketplace.MarketplaceInterfaceMock, t *testing.T) {
		//		if mockMPM == nil {
		//			t.Fatalf("expected MPM to not be nil, got: %+v", mockMPM)
		//		}
		//		if len(mockMPM.CreateSubscriptionCalls()) != 1 {
		//			t.Fatalf("expected 1 call to mockMPM.CreateSubscription, got: %d", len(mockMPM.CreateSubscriptionCalls()))
		//		}
		//	},
		//},
		//{
		//	Name:           "test subscription phase with error from mpm",
		//	ExpectedStatus: v1alpha1.PhaseFailed,
		//	ExpectedError:  "could not create subscription in namespace: codeready-workspaces: dummy error",
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseCreatingSubscription),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeMPM: &marketplace.MarketplaceInterfaceMock{
		//		CreateSubscriptionFunc: func(serverClient pkgclient.Client, os operatorsv1.OperatorSource, ns string, pkg string, channel string, operatorGroupNamespaces []string, approvalStrategy operatorsv1alpha1.Approval) error {
		//			return errors.New("dummy error")
		//		},
		//	},
		//	FakeControllerClient: fakepkgclient.NewFakeClient(),
		//	FakeConfig:           basicConfigMock(),
		//},
		//{
		//	Name:           "test creating components phase",
		//	ExpectedStatus: v1alpha1.PhaseInProgress,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseCreatingComponents),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme(), &aerogearv1.KeycloakRealm{
		//		ObjectMeta: metav1.ObjectMeta{
		//			Name:      "openshift",
		//			Namespace: "rhsso",
		//		},
		//	}, &threescalev1.APIManager{ObjectMeta: metav1.ObjectMeta{Name: apiManagerName, Namespace: defaultInstallationNamespace}}),
		//	FakeConfig: basicConfigMock(),
		//},
		//{
		//	Name:           "test creating components phase missing cluster",
		//	ExpectedStatus: v1alpha1.PhaseInProgress,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseCreatingComponents),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme(), &aerogearv1.KeycloakRealm{
		//		ObjectMeta: metav1.ObjectMeta{
		//			Name:      "openshift",
		//			Namespace: "rhsso",
		//		},
		//	}),
		//	FakeConfig: basicConfigMock(),
		//},
		//{
		//	Name:           "test awaiting operator phase, not yet ready",
		//	ExpectedStatus: v1alpha1.PhaseAwaitingOperator,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseAwaitingOperator),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme()),
		//	FakeMPM: &marketplace.MarketplaceInterfaceMock{
		//		GetSubscriptionInstallPlanFunc: func(subName string, ns string) (plan *operatorsv1alpha1.InstallPlan, e error) {
		//			return &operatorsv1alpha1.InstallPlan{
		//				ObjectMeta: metav1.ObjectMeta{
		//					Namespace: ns,
		//					Name:      subName,
		//				},
		//			}, nil
		//		},
		//	},
		//	FakeConfig: basicConfigMock(),
		//	ValidateCallCounts: func(mockConfig *config.ConfigReadWriterMock, mockMPM *marketplace.MarketplaceInterfaceMock, t *testing.T) {
		//		if mockMPM == nil {
		//			t.Fatalf("expected MPM to not be nil, got: %+v", mockMPM)
		//		}
		//		if len(mockMPM.GetSubscriptionInstallPlanCalls()) != 1 {
		//			t.Fatalf("expected 1 call to mockMPM.GetSubscriptionInstallPlan, got: %d", len(mockMPM.GetSubscriptionInstallPlanCalls()))
		//		}
		//	},
		//},
		//{
		//	Name:           "test awaiting operator phase, ready",
		//	ExpectedStatus: v1alpha1.PhaseCreatingComponents,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseAwaitingOperator),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme()),
		//	FakeMPM: &marketplace.MarketplaceInterfaceMock{
		//		GetSubscriptionInstallPlanFunc: func(subName string, ns string) (plan *operatorsv1alpha1.InstallPlan, e error) {
		//			return &operatorsv1alpha1.InstallPlan{
		//				ObjectMeta: metav1.ObjectMeta{
		//					Namespace: ns,
		//					Name:      subName,
		//				},
		//				Status: operatorsv1alpha1.InstallPlanStatus{
		//					Phase: operatorsv1alpha1.InstallPlanPhaseComplete,
		//				},
		//			}, nil
		//		},
		//	},
		//	FakeConfig: basicConfigMock(),
		//	ValidateCallCounts: func(mockConfig *config.ConfigReadWriterMock, mockMPM *marketplace.MarketplaceInterfaceMock, t *testing.T) {
		//		if mockMPM == nil {
		//			t.Fatalf("expected MPM to not be nil, got: %+v", mockMPM)
		//		}
		//		if len(mockMPM.GetSubscriptionInstallPlanCalls()) != 1 {
		//			t.Fatalf("expected 1 call to mockMPM.GetSubscriptionInstallPlan, got: %d", len(mockMPM.GetSubscriptionInstallPlanCalls()))
		//		}
		//	},
		//},
		//{
		//	Name:           "test in progress phase, not ready",
		//	ExpectedStatus: v1alpha1.PhaseInProgress,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseInProgress),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme(), &aerogearv1.KeycloakRealm{
		//		ObjectMeta: metav1.ObjectMeta{
		//			Name:      "openshift",
		//			Namespace: "rhsso",
		//		},
		//	}, &threescalev1.APIManager{ObjectMeta: metav1.ObjectMeta{Name: apiManagerName, Namespace: defaultInstallationNamespace}}),
		//	FakeConfig: basicConfigMock(),
		//},
		//{
		//	Name:           "test in progress phase, ready",
		//	ExpectedStatus: v1alpha1.PhaseCompleted,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseInProgress),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme(), &aerogearv1.KeycloakRealm{
		//		ObjectMeta: metav1.ObjectMeta{
		//			Name:      "openshift",
		//			Namespace: "rhsso",
		//		},
		//	}, &threescalev1.APIManager{
		//		ObjectMeta: metav1.ObjectMeta{Name: apiManagerName, Namespace: defaultInstallationNamespace},
		//		Status: threescalev1.APIManagerStatus{
		//			Deployments: olm.DeploymentStatus{
		//				Ready:    []string{},
		//				Starting: []string{"Deployment count is greater than zero"},
		//				Stopped:  []string{"Deployment count is greater than zero"},
		//			},
		//		},
		//	}),
		//	FakeConfig: basicConfigMock(),
		//},
		//{
		//	Name:           "test completed phase",
		//	ExpectedStatus: v1alpha1.PhaseCompleted,
		//	Object: &v1alpha1.Installation{
		//		Status: v1alpha1.InstallationStatus{
		//			ProductStatus: map[v1alpha1.ProductName]string{
		//				v1alpha1.Product3Scale: string(v1alpha1.PhaseCompleted),
		//			},
		//		},
		//	},
		//	FakeAppsV1Client:     &fakeappsv1Client.FakeAppsV1{},
		//	FakeOauthClient:      &fakeoauthClient.FakeOauthV1{},
		//	FakeControllerClient: fakepkgclient.NewFakeClientWithScheme(buildScheme(), &threescalev1.APIManager{
		//		ObjectMeta: metav1.ObjectMeta{Name: apiManagerName, Namespace: defaultInstallationNamespace},
		//		Status: threescalev1.APIManagerStatus{
		//			Deployments: olm.DeploymentStatus{
		//				Ready:    []string{"Deployment count is greater than zero"},
		//				Starting: []string{},
		//				Stopped:  []string{},
		//			},
		//		},
		//	}),
		//	FakeMPM: &marketplace.MarketplaceInterfaceMock{
		//		CreateSubscriptionFunc: func(serverClient pkgclient.Client, os marketplacev1.OperatorSource, ns string, pkg string, channel string, operatorGroupNamespaces []string, approvalStrategy operatorsv1alpha1.Approval) error {
		//			return nil
		//		},
		//	},
		//	FakeConfig: basicConfigMock(),
		//	ValidateCallCounts: func(mockConfig *config.ConfigReadWriterMock, mockMPM *marketplace.MarketplaceInterfaceMock, t *testing.T) {
		//		if mockMPM == nil {
		//			t.Fatalf("expected MPM to not be nil, got: %+v", mockMPM)
		//		}
		//		if len(mockMPM.CreateSubscriptionCalls()) != 1 {
		//			t.Fatalf("expected 1 call to mockMPM.CreateSubscription, got: %d", len(mockMPM.CreateSubscriptionCalls()))
		//		}
		//	},
		//},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(*testing.T) {
			testReconciler, err := NewReconciler(
				scenario.FakeConfig,
				scenario.Object,
				scenario.FakeAppsV1Client,
				scenario.FakeOauthClient,
				scenario.FakeMPM,
			)
			if err != nil && err.Error() != scenario.ExpectedCreateError {
				t.Fatalf("unexpected error creating reconciler: '%v', expected: '%v'", err, scenario.ExpectedCreateError)
			}

			if err == nil && scenario.ExpectedCreateError != "" {
				t.Fatalf("expected error '%v' and got nil", scenario.ExpectedCreateError)
			}

			// if we expect errors creating the reconciler, don't try to use it
			if scenario.ExpectedCreateError != "" {
				return
			}

			status, err := testReconciler.Reconcile(scenario.Object, scenario.FakeControllerClient)
			if err != nil && err.Error() != scenario.ExpectedError {
				t.Fatalf("unexpected error: %v, expected: %v", err, scenario.ExpectedError)
			}

			if err == nil && scenario.ExpectedError != "" {
				t.Fatalf("expected error '%v' and got nil", scenario.ExpectedError)
			}

			if status != scenario.ExpectedStatus {
				t.Fatalf("Expected status: '%v', got: '%v'", scenario.ExpectedStatus, status)
			}

			if scenario.ValidateCallCounts != nil {
				scenario.ValidateCallCounts(scenario.FakeConfig, scenario.FakeMPM, t)
			}
		})
	}
}
