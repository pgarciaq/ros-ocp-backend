package quota

import libquota "github.com/redhatinsights/ros-ocp-backend/librobne/quota"

const (
	QuotaRecTypeTighten         = libquota.QuotaRecTypeTighten
	QuotaRecTypeRaise           = libquota.QuotaRecTypeRaise
	QuotaRecTypeOptimal         = libquota.QuotaRecTypeOptimal
	QuotaRecTypeNone            = libquota.QuotaRecTypeNone
	QuotaRiskHigh               = libquota.QuotaRiskHigh
	QuotaRiskMedium             = libquota.QuotaRiskMedium
	QuotaRiskLow                = libquota.QuotaRiskLow
	QuotaRiskNone               = libquota.QuotaRiskNone
	QuotaContainerTerm          = libquota.QuotaContainerTerm
	QuotaContainerEngine        = libquota.QuotaContainerEngine
	NotifQuotaNearCapacity      = libquota.NotifQuotaNearCapacity
	NotifQuotaOversized         = libquota.NotifQuotaOversized
	NotifQuotaBlocking          = libquota.NotifQuotaBlocking
	NotifClusterQuotaAtCapacity = libquota.NotifClusterQuotaAtCapacity
)

type QuotaRecConfig = libquota.QuotaRecConfig
type NamespaceQuotaSnapshot = libquota.NamespaceQuotaSnapshot
type ContainerQuotaAggregate = libquota.ContainerQuotaAggregate
type QuotaRec = libquota.QuotaRec
type QuotaResourceBundle = libquota.QuotaResourceBundle
type QuotaUtilizationBP = libquota.QuotaUtilizationBP
type QuotaCapacityFreed = libquota.QuotaCapacityFreed
type ClusterQuotaSnapshot = libquota.ClusterQuotaSnapshot
type NamespaceQuotaClusterAggregate = libquota.NamespaceQuotaClusterAggregate
type ClusterQuotaRec = libquota.ClusterQuotaRec

var (
	ComputeQuotaRecommendation        = libquota.ComputeQuotaRecommendation
	ComputeClusterQuotaRecommendation = libquota.ComputeClusterQuotaRecommendation
	QuotaNotificationCodes            = libquota.QuotaNotificationCodes
	ClusterQuotaNotificationCodes     = libquota.ClusterQuotaNotificationCodes
	UtilizationBP                     = libquota.UtilizationBP
	BPToPercentInt                    = libquota.BPToPercentInt
)
