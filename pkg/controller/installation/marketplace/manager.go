package marketplace

import (
	"context"
	"github.com/integr8ly/integreatly-operator/pkg/apis/integreatly/v1alpha1"
	coreosv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1"
	coreosv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/ownerutil"
	marketplacev1 "github.com/operator-framework/operator-marketplace/pkg/apis/operators/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	providerLabel      = "opsrc-provider"
	IntegreatlyChannel = "integreatly"
)

type operatorSources struct {
	Integreatly marketplacev1.OperatorSource
}

func GetOperatorSources() *operatorSources {
	return &operatorSources{
		Integreatly: marketplacev1.OperatorSource{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					providerLabel: "integreatly",
				},
			},
			Spec: marketplacev1.OperatorSourceSpec{
				DisplayName: "Integreatly Operators",
				Publisher:   "Integreatly",
			},
		},
	}
}

//go:generate moq -out MarketplaceManager_moq.go . MarketplaceInterface
type MarketplaceInterface interface {
	CreateSubscription(ctx context.Context, serverClient pkgclient.Client, os marketplacev1.OperatorSource, ns string, pkg string, channel string, operatorGroupNamespaces []string, approvalStrategy coreosv1alpha1.Approval) error
	GetSubscriptionInstallPlan(ctx context.Context, serverClient pkgclient.Client, subName, ns string) (*coreosv1alpha1.InstallPlan, *coreosv1alpha1.Subscription, error)
}

type MarketplaceManager struct {
	owner      ownerutil.Owner
}

func NewManager(install *v1alpha1.Installation) *MarketplaceManager {
	return &MarketplaceManager{
		owner:      install,
	}
}

func (m *MarketplaceManager) CreateSubscription(ctx context.Context, serverClient pkgclient.Client, os marketplacev1.OperatorSource, ns string, pkg string, channel string, operatorGroupNamespaces []string, approvalStrategy coreosv1alpha1.Approval) error {
	logrus.Infof("creating subscription in ns: %s", ns)
	sub := &coreosv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      pkg,
		},
	}

	err := serverClient.Get(context.TODO(), pkgclient.ObjectKey{Name: sub.Name, Namespace: sub.Namespace}, sub)
	if err == nil {
		logrus.Infof("Subscription already exists")
		return k8serr.NewAlreadyExists(coreosv1alpha1.Resource("subscription"), sub.Name)
	}

	csc := &marketplacev1.CatalogSourceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "installed-" + os.Labels[providerLabel] + "-" + ns,
			Namespace: "openshift-marketplace",
		},
		Spec: marketplacev1.CatalogSourceConfigSpec{
			DisplayName:     os.Spec.DisplayName,
			Publisher:       os.Spec.Publisher,
			Packages:        pkg,
			TargetNamespace: ns,
		},
	}
	//TODO might need to check status of catalogsourceconfig
	_, err := m.getSubscription(ctx, sub.Name, ns)
	if err != nil && k8serr.IsNotFound(err) {
		logrus.Infof("Subscription not found ")
		// delete catalog source config
		if err := m.client.Delete(ctx, csc, func(options *pkgclient.DeleteOptions) {
			gp := int64(0)
			options.GracePeriodSeconds = &gp
		}); err != nil && !k8serr.IsNotFound(err) {
			return errors.Wrap(err, "failed to delete the catalog source config "+err.Error()+" \n")
		}
	}

	ownerutil.EnsureOwner(csc, m.owner)
	err = m.client.Create(ctx, csc)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		logrus.Infof("error creating catalog source config: %s", err.Error())
		return err
	}
	logrus.Infof("catalog source config created")

	og := &coreosv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      ns + "-integreatly",
			Labels:    map[string]string{"integreatly": pkg},
		},
		Spec: coreosv1.OperatorGroupSpec{
			TargetNamespaces: operatorGroupNamespaces,
		},
	}
	ownerutil.EnsureOwner(og, m.owner)
	err = m.client.Create(ctx, og)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		logrus.Infof("error creating operator group")
		return err
	}

	sub.Spec = &coreosv1alpha1.SubscriptionSpec{
		InstallPlanApproval:    approvalStrategy,
		Channel:                channel,
		Package:                pkg,
		CatalogSource:          csc.Name,
		CatalogSourceNamespace: ns,
	}
	ownerutil.EnsureOwner(sub, m.owner)
	err = m.client.Create(ctx, sub)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		logrus.Infof("error creating sub")
		return err
	}

	logrus.Infof("no errors subscription ready")

	return nil
}

func (m *MarketplaceManager) getSubscription(ctx context.Context, subName, ns string) (*coreosv1alpha1.Subscription, error) {
	logrus.Infof("Getting subscription %s in ns: %s", subName, ns)
	sub := &coreosv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      subName,
		},
	}
	serverClient, err := pkgclient.New(m.restConfig, pkgclient.Options{})
	if err != nil {
		logrus.Infof("Error creating server client")
		return nil, err
	}

	err = serverClient.Get(ctx, pkgclient.ObjectKey{Name: sub.Name, Namespace: sub.Namespace}, sub)
	if err != nil {
		logrus.Infof("Error getting subscription %s in ns: %s", subName, ns)
		return nil, err
	}
	return sub, nil
}

func (m *MarketplaceManager) GetSubscriptionInstallPlan(ctx context.Context, serverClient pkgclient.Client, subName, ns string) (*coreosv1alpha1.InstallPlan, *coreosv1alpha1.Subscription, error) {
	sub, err := m.getSubscription(ctx, subName, ns)
	if err != nil {
		return nil, nil, errors.Wrap(err, "GetSubscriptionInstallPlan")
	}
	if sub.Status.Install == nil {
		logrus.Infof("Installplan for subscription %s is not yet created", sub.Name)
		return nil, sub, k8serr.NewNotFound(coreosv1alpha1.Resource("installplan"), "")
	}

	ip := &coreosv1alpha1.InstallPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sub.Status.Install.Name,
			Namespace: ns,
		},
	}
	serverClient, err := pkgclient.New(m.restConfig, pkgclient.Options{})
	if err != nil {
		logrus.Infof("Error creating server client")
		return nil, nil, err
	}
	err = serverClient.Get(ctx, pkgclient.ObjectKey{Name: ip.Name, Namespace: ip.Namespace}, ip)
	if err != nil {
		return nil, nil, err
	}

	return ip, sub, err
}
