package solutionexplorer

import (
	"context"
	"fmt"
	"strings"

	webapp "github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/tutorial-web-app-operator/pkg/apis/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/marketplace"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/products/config"
	"github.com/integr8ly/integreatly-operator/pkg/resources"
	v1 "github.com/openshift/api/apps/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	oauthClient "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	pkgclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	defaultName             = "solution-explorer"
	defaultSubNameAndPkg    = "integreatly-solution-explorer"
	defaultTemplateLoc      = "/home/tutorial-web-app-operator/deploy/template/tutorial-web-app.yml"
	paramOpenShiftHost      = "OPENSHIFT_HOST"
	paramOpenShiftOauthHost = "OPENSHIFT_OAUTH_HOST"
	paramOauthClient        = "OPENSHIFT_OAUTHCLIENT_ID"
	paramOpenShiftVersion   = "OPENSHIFT_VERSION"
	paramSSORoute           = "SSO_ROUTE"
	defaultRouteName        = "tutorial-web-app"
	finalizer               = "finalizer.webapp.integreatly.org"
	oauthId                 = "integreatly-solution-explorer"
)

type Reconciler struct {
	*resources.Reconciler
	coreClient    kubernetes.Interface
	oauthv1Client oauthClient.OauthV1Interface
	Config        *config.SolutionExplorer
	ConfigManager config.ConfigReadWriter
	mpm           marketplace.MarketplaceInterface
	logger        *logrus.Entry
	OauthResolver OauthResolver
}

//go:generate moq -out OauthResolver_moq.go . OauthResolver
type OauthResolver interface {
	GetOauthEndPoint() (*resources.OauthServerConfig, error)
}

func NewReconciler(configManager config.ConfigReadWriter, instance *v1alpha1.Installation, oauthv1Client oauthClient.OauthV1Interface, mpm marketplace.MarketplaceInterface, resolver OauthResolver) (*Reconciler, error) {
	seConfig, err := configManager.ReadSolutionExplorer()
	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve solution explorer config")
	}

	if seConfig.GetNamespace() == "" {
		seConfig.SetNamespace(instance.Spec.NamespacePrefix + defaultName)
	}
	if err = seConfig.Validate(); err != nil {
		return nil, errors.Wrap(err, "solution explorer config is not valid")
	}

	logger := logrus.NewEntry(logrus.StandardLogger())

	return &Reconciler{
		ConfigManager: configManager,
		Config:        seConfig,
		mpm:           mpm,
		logger:        logger,
		Reconciler:    resources.NewReconciler(mpm),
		OauthResolver: resolver,
		oauthv1Client: oauthv1Client,
	}, nil
}

func (r *Reconciler) GetPreflightObject(ns string) runtime.Object {
	return &v1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tutorial-web-app",
			Namespace: ns,
		},
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, inst *v1alpha1.Installation, product *v1alpha1.InstallationProductStatus, serverClient pkgclient.Client) (v1alpha1.StatusPhase, error) {
	logrus.Info("Reconciling solution explorer")

	// Add finalizer if not there
	err := resources.AddFinalizer(ctx, inst, serverClient, finalizer)
	if err != nil {
		logrus.Error("Error adding solution explorer finalizer to installation", err)
		return v1alpha1.PhaseFailed, err
	}

	// Run finalization logic. If it fails, don't remove the finalizer
	// so that we can retry during the next reconciliation
	if inst.GetDeletionTimestamp() != nil {
		err := resources.RemoveOauthClient(ctx, inst, serverClient, r.oauthv1Client, finalizer, oauthId)
		if err != nil && !k8serr.IsNotFound(err) {
			logrus.Error("Error removing solution explorer oauth client", err)
			return v1alpha1.PhaseFailed, err
		}
	}

	phase, err := r.ReconcileNamespace(ctx, r.Config.GetNamespace(), inst, serverClient)
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.ReconcileSubscription(ctx, inst, marketplace.Target{Pkg: defaultSubNameAndPkg, Channel: marketplace.IntegreatlyChannel, Namespace: r.Config.GetNamespace()}, serverClient)
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.ReconcileCustomResource(ctx, inst, serverClient)
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	route, err := r.ensureAppUrl(ctx, serverClient)
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}

	if r.Config.GetHost() != route {
		r.Config.SetHost(route)
		r.ConfigManager.WriteConfig(r.Config)
	}

	phase, err = r.ReconcileOauthClient(ctx, inst, &oauthv1.OAuthClient{
		RedirectURIs: []string{route},
		Secret:       "test",
		GrantMethod:  oauthv1.GrantHandlerAuto,
		ObjectMeta: metav1.ObjectMeta{
			Name: oauthId,
		},
	}, serverClient)
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	product.Host = r.Config.GetHost()
	product.Version = r.Config.GetProductVersion()

	return v1alpha1.PhaseCompleted, nil
}

func (r *Reconciler) ensureAppUrl(ctx context.Context, client pkgclient.Client) (string, error) {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Config.GetNamespace(),
			Name:      defaultRouteName,
		},
	}
	if err := client.Get(ctx, pkgclient.ObjectKey{Name: route.Name, Namespace: route.Namespace}, route); err != nil {
		return "", errors.Wrap(err, "failed to get route for solution explorer")
	}
	protocol := "https"
	if route.Spec.TLS == nil {
		protocol = "http"
	}

	return fmt.Sprintf("%s://%s", protocol, route.Spec.Host), nil
}

func (r *Reconciler) ReconcileCustomResource(ctx context.Context, inst *v1alpha1.Installation, client pkgclient.Client) (v1alpha1.StatusPhase, error) {
	//todo shouldn't need to do this with each reconcile
	oauthConfig, err := r.OauthResolver.GetOauthEndPoint()
	if err != nil {
		return v1alpha1.PhaseFailed, errors.Wrap(err, "failed to get oauth details ")
	}
	ssoConfig, err := r.ConfigManager.ReadRHSSO()
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	seCR := &webapp.WebApp{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.Config.GetNamespace(),
			Name:      defaultName,
		},
	}
	ownerutil.AddOwner(seCR, inst, true, true)
	oauthURL := strings.Replace(strings.Replace(oauthConfig.AuthorizationEndpoint, "https://", "", 1), "/oauth/authorize", "", 1)
	logrus.Info("ReconcileCustomResource setting url for openshift host ", oauthURL)

	_, err = controllerutil.CreateOrUpdate(ctx, client, seCR, func(existing runtime.Object) error {
		cr := existing.(*webapp.WebApp)
		cr.Spec.AppLabel = "tutorial-web-app"
		cr.Spec.Template.Path = defaultTemplateLoc
		cr.Spec.Template.Parameters = map[string]string{
			paramOauthClient:        defaultSubNameAndPkg,
			paramSSORoute:           ssoConfig.GetHost(),
			paramOpenShiftHost:      inst.Spec.MasterURL,
			paramOpenShiftOauthHost: oauthURL,
			paramOpenShiftVersion:   "4",
		}
		return nil
	})
	if err != nil {
		return v1alpha1.PhaseFailed, errors.Wrap(err, "failed to reconcile webapp resource")
	}
	// do a get to ensure we have an upto date copy
	if err := client.Get(ctx, pkgclient.ObjectKey{Namespace: seCR.Namespace, Name: seCR.Name}, seCR); err != nil {
		// any error here is bad as it should exist now
		return v1alpha1.PhaseFailed, errors.Wrap(err, fmt.Sprintf("failed to get the webapp resource namespace %s name %s", seCR.Namespace, seCR.Name))
	}
	if seCR.Status.Message == "OK" {
		if r.Config.GetProductVersion() != v1alpha1.ProductVersion(seCR.Status.Version) {
			r.Config.SetProductVersion(seCR.Status.Version)
			r.ConfigManager.WriteConfig(r.Config)
		}
		return v1alpha1.PhaseCompleted, nil
	}
	return v1alpha1.PhaseInProgress, nil

}
