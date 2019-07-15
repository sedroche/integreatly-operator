package threescale

import (
	"context"
	"fmt"
	threescalev1 "github.com/integr8ly/integreatly-operator/pkg/apis/3scale/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/marketplace"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/products/config"
	"github.com/integr8ly/integreatly-operator/pkg/controller/installation/products/rhsso"
	coreosv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"k8s.io/client-go/rest"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	aerogearv1 "github.com/integr8ly/integreatly-operator/pkg/apis/aerogear/v1alpha1"
	appsv1 "github.com/openshift/api/apps/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1Client "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	oauthClient "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultInstallationNamespace = "3scale"
	packageName                  = "3scale"
	apiManagerName               = "3scale"
	clientId                     = "3scale"
	oauthId                      = "3scale"
	clientSecret                 = clientId + "-client-secret"
)

func NewReconciler(client pkgclient.Client, rc *rest.Config, configManager config.ConfigReadWriter, i *v1alpha1.Installation, mgr manager.Manager) (*Reconciler, error) {
	mpm := marketplace.NewManager(client, mgr, rc)
	ns := i.Spec.NamespacePrefix + defaultInstallationNamespace

	httpc := &http.Client{}
	tsClient := NewThreeScaleClient(httpc, i.Spec.RoutingSubdomain, ns)

	appsv1Client, err := appsv1Client.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	oauthv1Client, err := oauthClient.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	return &Reconciler{
		client:        client,
		restConfig:    rc,
		ConfigManager: configManager,
		mpm:           mpm,
		mgr:           mgr,
		namespace:     ns,
		installation:  i,
		tsClient:      tsClient,
		appsv1Client:  appsv1Client,
		oauthv1Client: oauthv1Client,
	}, nil
}

type Reconciler struct {
	client        pkgclient.Client
	restConfig    *rest.Config
	namespace     string
	ConfigManager config.ConfigReadWriter
	mpm           marketplace.MarketplaceInterface
	mgr           manager.Manager
	installation  *v1alpha1.Installation
	tsClient      *threeScaleClient
	appsv1Client  *appsv1Client.AppsV1Client
	oauthv1Client *oauthClient.OauthV1Client
}

func (r *Reconciler) Reconcile(in *v1alpha1.Installation) (v1alpha1.StatusPhase, error) {
	logrus.Infof("Reconciling %s", packageName)

	phase, err := r.reconcileNamespace()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.reconcileOperator()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.reconcileComponents()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	logrus.Infof("%s has successfully deployed", packageName)

	phase, err = r.reconcileRHSSOIntegration()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.reconcileUpdatingAdminDetails()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	phase, err = r.reconcileServiceDiscovery()
	if err != nil || phase != v1alpha1.PhaseCompleted {
		return phase, err
	}

	logrus.Infof("%s has reconciled successfully", packageName)
	return v1alpha1.PhaseCompleted, nil
}

func (r *Reconciler) reconcileNamespace() (v1alpha1.StatusPhase, error) {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.namespace,
			Labels: map[string]string{
				"integreatly": "yes",
			},
		},
	}
	err := r.client.Get(context.TODO(), pkgclient.ObjectKey{Name: r.namespace}, ns)
	if err != nil && !k8serr.IsNotFound(err) {
		return v1alpha1.PhaseFailed, err
	}

	if err != nil {
		logrus.Infof("Namespace %s not present", r.namespace)
		if err := controllerutil.SetControllerReference(r.installation, ns, r.mgr.GetScheme()); err != nil {
			return v1alpha1.PhaseFailed, err
		}
		err := r.client.Create(context.TODO(), ns)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
		logrus.Infof("%s namespace created", r.namespace)
	}

	if ns.Status.Phase == v1.NamespaceActive {
		return v1alpha1.PhaseCompleted, nil
	}

	return v1alpha1.PhaseInProgress, nil
}

func (r *Reconciler) reconcileOperator() (v1alpha1.StatusPhase, error) {
	err := r.mpm.CreateSubscription(
		r.installation,
		marketplace.GetOperatorSources().Integreatly,
		r.namespace,
		packageName,
		"integreatly",
		[]string{r.namespace},
		coreosv1alpha1.ApprovalAutomatic)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return v1alpha1.PhaseFailed, err
	}

	ip, err := r.mpm.GetSubscriptionInstallPlan(packageName, r.namespace)
	if ip != nil && ip.Status.Phase == coreosv1alpha1.InstallPlanPhaseComplete {
		return v1alpha1.PhaseCompleted, nil
	}

	return v1alpha1.PhaseInProgress, nil
}

func (r *Reconciler) reconcileComponents() (v1alpha1.StatusPhase, error) {
	bucket := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "s3-bucket",
		},
	}

	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: bucket.Name, Namespace: r.ConfigManager.GetOperatorNamespace()}, bucket)
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}

	s3SecretName := "s3-credentials"
	tsS3 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s3SecretName,
			Namespace: r.namespace,
		},
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: tsS3.Name, Namespace: tsS3.Namespace}, tsS3)
	if err != nil && k8serr.IsNotFound(err) {
		// We are copying the s3 details for now but this is not ideal as the secrets can get out of sync.
		// We need to revise how this secret is set
		s3 := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: s3SecretName,
			},
		}
		err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: s3.Name, Namespace: r.ConfigManager.GetOperatorNamespace()}, s3)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}

		tsS3.Data = s3.Data
		err = serverClient.Create(context.TODO(), tsS3)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
	}

	resourceRequirements := false
	apim := &threescalev1.APIManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiManagerName,
			Namespace: r.namespace,
			Labels: map[string]string{
				"integreatly": "yes",
			},
		},
		Spec: threescalev1.APIManagerSpec{
			APIManagerCommonSpec: threescalev1.APIManagerCommonSpec{
				WildcardDomain:              r.installation.Spec.RoutingSubdomain,
				ResourceRequirementsEnabled: &resourceRequirements,
			},
			System: &threescalev1.SystemSpec{
				FileStorageSpec: &threescalev1.SystemFileStorageSpec{
					S3: &threescalev1.SystemS3Spec{
						AWSBucket: string(bucket.Data["AWS_BUCKET"]),
						AWSRegion: string(bucket.Data["AWS_REGION"]),
						AWSCredentials: v1.LocalObjectReference{
							Name: s3SecretName,
						},
					},
				},
			},
		},
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: apim.Name, Namespace: r.namespace}, apim)
	if err != nil && !k8serr.IsNotFound(err) {
		return v1alpha1.PhaseFailed, err
	}

	if err != nil {
		logrus.Infof("Creating API Manager")
		err := serverClient.Create(context.TODO(), apim)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
	} else {
		if len(apim.Status.Deployments.Starting) == 0 && len(apim.Status.Deployments.Stopped) == 0 && len(apim.Status.Deployments.Ready) > 0 {
			return v1alpha1.PhaseCompleted, nil
		}
	}

	return v1alpha1.PhaseInProgress, nil
}

func (r *Reconciler) reconcileRHSSOIntegration() (v1alpha1.StatusPhase, error) {
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	kcr := &aerogearv1.KeycloakRealm{}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: rhsso.KeycloakRealmName, Namespace: r.installation.Spec.NamespacePrefix + rhsso.DefaultRhssoNamespace}, kcr)
	if err != nil && !k8serr.IsNotFound(err) {
		return v1alpha1.PhaseFailed, err
	}

	if !containsClient(kcr.Spec.Clients, clientId) {
		logrus.Infof("Adding keycloak realm client")

		kcr.Spec.Clients = append(kcr.Spec.Clients, &aerogearv1.KeycloakClient{
			KeycloakApiClient: &aerogearv1.KeycloakApiClient{
				ID:                      clientId,
				ClientID:                clientId,
				Enabled:                 true,
				Secret:                  clientSecret,
				ClientAuthenticatorType: "client-secret",
				RedirectUris: []string{
					fmt.Sprintf("https://3scale-admin.%s/*", r.installation.Spec.RoutingSubdomain),
				},
				StandardFlowEnabled: true,
				RootURL:             fmt.Sprintf("https://3scale-admin.%s", r.installation.Spec.RoutingSubdomain),
				FullScopeAllowed:    true,
				Access: map[string]bool{
					"view":      true,
					"configure": true,
					"manage":    true,
				},
				ProtocolMappers: []aerogearv1.KeycloakProtocolMapper{
					{
						Name:            "given name",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-usermodel-property-mapper",
						ConsentRequired: true,
						ConsentText:     "${givenName}",
						Config: map[string]string{
							"userinfo.token.claim": "true",
							"user.attribute":       "firstName",
							"id.token.claim":       "true",
							"access.token.claim":   "true",
							"claim.name":           "given_name",
							"jsonType.label":       "String",
						},
					},
					{
						Name:            "email verified",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-usermodel-property-mapper",
						ConsentRequired: true,
						ConsentText:     "${emailVerified}",
						Config: map[string]string{
							"userinfo.token.claim": "true",
							"user.attribute":       "emailVerified",
							"id.token.claim":       "true",
							"access.token.claim":   "true",
							"claim.name":           "email_verified",
							"jsonType.label":       "String",
						},
					},
					{
						Name:            "full name",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-full-name-mapper",
						ConsentRequired: true,
						ConsentText:     "${fullName}",
						Config: map[string]string{
							"id.token.claim":     "true",
							"access.token.claim": "true",
						},
					},
					{
						Name:            "family name",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-usermodel-property-mapper",
						ConsentRequired: true,
						ConsentText:     "${familyName}",
						Config: map[string]string{
							"userinfo.token.claim": "true",
							"user.attribute":       "lastName",
							"id.token.claim":       "true",
							"access.token.claim":   "true",
							"claim.name":           "family_name",
							"jsonType.label":       "String",
						},
					},
					{
						Name:            "role list",
						Protocol:        "saml",
						ProtocolMapper:  "saml-role-list-mapper",
						ConsentRequired: false,
						ConsentText:     "${familyName}",
						Config: map[string]string{
							"single":               "false",
							"attribute.nameformat": "Basic",
							"attribute.name":       "Role",
						},
					},
					{
						Name:            "email",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-usermodel-property-mapper",
						ConsentRequired: true,
						ConsentText:     "${email}",
						Config: map[string]string{
							"userinfo.token.claim": "true",
							"user.attribute":       "email",
							"id.token.claim":       "true",
							"access.token.claim":   "true",
							"claim.name":           "email",
							"jsonType.label":       "String",
						},
					},
					{
						Name:            "org_name",
						Protocol:        "openid-connect",
						ProtocolMapper:  "oidc-usermodel-property-mapper",
						ConsentRequired: false,
						Config: map[string]string{
							"userinfo.token.claim": "true",
							"user.attribute":       "org_name",
							"id.token.claim":       "true",
							"access.token.claim":   "true",
							"claim.name":           "org_name",
							"jsonType.label":       "String",
						},
					},
				},
			},
			OutputSecret: clientId + "-secret",
		})

		err = serverClient.Update(context.TODO(), kcr)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
	}

	urlSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "credential-rhsso",
		},
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: urlSecret.Name, Namespace: r.installation.Spec.NamespacePrefix + rhsso.DefaultRhssoNamespace}, urlSecret)
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}

	accessToken, err := r.GetAdminToken()
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	site := string(urlSecret.Data["SSO_ADMIN_URL"]) + "/auth/realms/" + rhsso.KeycloakRealmName
	res, err := r.tsClient.AddSSOIntegration(map[string]string{
		"kind":                              "keycloak",
		"name":                              "rhsso",
		"client_id":                         clientId,
		"client_secret":                     clientSecret,
		"site":                              site,
		"skip_ssl_certificate_verification": "true",
		"published":                         "true",
	}, *accessToken)

	if err != nil || res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusUnprocessableEntity {
		return v1alpha1.PhaseFailed, err
	}

	return v1alpha1.PhaseCompleted, nil
}

func (r *Reconciler) reconcileUpdatingAdminDetails() (v1alpha1.StatusPhase, error) {
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	kcr := &aerogearv1.KeycloakRealm{}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: rhsso.KeycloakRealmName, Namespace: r.installation.Spec.NamespacePrefix + rhsso.DefaultRhssoNamespace}, kcr)
	if err != nil && !k8serr.IsNotFound(err) {
		return v1alpha1.PhaseFailed, err
	}

	kcUsers := filterUsers(kcr.Spec.Users, func(u *aerogearv1.KeycloakUser) bool {
		return u.UserName == rhsso.CustomerAdminName
	})
	if len(kcUsers) == 1 {
		accessToken, err := r.GetAdminToken()
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
		tsAdmin, err := r.tsClient.GetAdminUser(*accessToken)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}

		kcCaUser := kcUsers[0]
		if tsAdmin.UserDetails.Username != kcCaUser.UserName && tsAdmin.UserDetails.Email != kcCaUser.Email {
			res, err := r.tsClient.UpdateAdminPortalUserDetails(kcCaUser.UserName, kcCaUser.Email, *accessToken)
			if err != nil || res.StatusCode != http.StatusOK && res.StatusCode != http.StatusUnprocessableEntity {
				return v1alpha1.PhaseFailed, err
			}
		}

		currentUsername, currentEmail, err := r.GetAdminNameAndPassFromSecret()
		if *currentUsername != kcCaUser.UserName || *currentEmail != kcCaUser.Email {
			err = r.SetAdminDetailsOnSecret(kcCaUser.UserName, kcCaUser.Email)
			if err != nil {
				return v1alpha1.PhaseFailed, err
			}

			err = r.RolloutDeployment("system-app")
			if err != nil {
				return v1alpha1.PhaseFailed, err
			}
		}
	}

	return v1alpha1.PhaseCompleted, nil
}

func (r *Reconciler) reconcileServiceDiscovery() (v1alpha1.StatusPhase, error) {
	_, err := r.oauthv1Client.OAuthClients().Get(oauthId, metav1.GetOptions{})
	if err != nil && k8serr.IsNotFound(err) {
		tsOauth := &oauthv1.OAuthClient{
			ObjectMeta: metav1.ObjectMeta{
				Name: oauthId,
			},
			Secret: clientSecret,
			RedirectURIs: []string{
				r.installation.Spec.MasterURL,
			},
			GrantMethod: oauthv1.GrantHandlerPrompt,
		}
		if err := controllerutil.SetControllerReference(r.installation, tsOauth, r.mgr.GetScheme()); err != nil {
			return v1alpha1.PhaseFailed, err
		}
		_, err = r.oauthv1Client.OAuthClients().Create(tsOauth)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
	}

	system := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system",
			Namespace: r.namespace,
		},
	}
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: system.Name, Namespace: system.Namespace}, system)
	if err != nil {
		return v1alpha1.PhaseFailed, err
	}
	sdConfig := fmt.Sprintf("production:\n  enabled: true\n  server_scheme: 'https'\n  server_host: 'kubernetes.default.svc.cluster.local'\n  server_port: 443\n  authentication_method: oauth\n  oauth_server_type: builtin\n  client_id: '%s'\n  client_secret: '%s'\n  timeout: 1\n  open_timeout: 1\n  max_retry: 5\n", oauthId, clientSecret)
	if system.Data["service_discovery.yml"] != sdConfig {
		system.Data["service_discovery.yml"] = sdConfig
		r.client.Update(context.TODO(), system)
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}

		err = r.RolloutDeployment("system-app")
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}

		err = r.RolloutDeployment("system-sidekiq")
		if err != nil {
			return v1alpha1.PhaseFailed, err
		}
	}

	return v1alpha1.PhaseCompleted, nil
}

func (r *Reconciler) GetAdminNameAndPassFromSecret() (*string, *string, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.namespace,
			Name:      "system-seed",
		},
	}
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return nil, nil, err
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: s.Name, Namespace: r.namespace}, s)
	if err != nil {
		return nil, nil, err
	}

	username := string(s.Data["ADMIN_USER"])
	email := string(s.Data["ADMIN_EMAIL"])
	return &username, &email, nil
}

func (r *Reconciler) SetAdminDetailsOnSecret(username string, email string) error {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.namespace,
			Name:      "system-seed",
		},
	}
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return err
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: s.Name, Namespace: r.namespace}, s)
	if err != nil {
		return err
	}

	currentAdminUser := string(s.Data["ADMIN_USER"])
	currentAdminEmail := string(s.Data["ADMIN_EMAIL"])
	if currentAdminUser == username && currentAdminEmail == email {
		return nil
	}

	s.Data["ADMIN_USER"] = []byte(username)
	s.Data["ADMIN_EMAIL"] = []byte(email)
	err = serverClient.Update(context.TODO(), s)
	if err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) GetAdminToken() (*string, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "system-seed",
		},
	}
	serverClient, err := pkgclient.New(r.restConfig, pkgclient.Options{})
	if err != nil {
		return nil, err
	}
	err = serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: s.Name, Namespace: r.namespace}, s)
	if err != nil {
		return nil, err
	}

	accessToken := string(s.Data["ADMIN_ACCESS_TOKEN"])
	return &accessToken, nil
}

func (r *Reconciler) RolloutDeployment(name string) error {
	_, err := r.appsv1Client.DeploymentConfigs(r.namespace).Instantiate(name, &appsv1.DeploymentRequest{
		Name:   name,
		Force:  true,
		Latest: true,
	})

	return err
}

func containsClient(kcc []*aerogearv1.KeycloakClient, id string) bool {
	for _, a := range kcc {
		if a.ID == id {
			return true
		}
	}
	return false
}

type predicateFunc func(*aerogearv1.KeycloakUser) bool

func filterUsers(u []*aerogearv1.KeycloakUser, predicate predicateFunc) []*aerogearv1.KeycloakUser {
	var result []*aerogearv1.KeycloakUser
	for _, s := range u {
		if predicate(s) {
			result = append(result, s)
		}
	}

	return result
}
