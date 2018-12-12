package models

import (
	"context"
	"database/sql"
	"encoding/json"

	"yunion.io/x/jsonutils"
	"yunion.io/x/log"
	"yunion.io/x/sqlchemy"

	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/httperrors"
	"yunion.io/x/onecloud/pkg/mcclient"
)

const (
	SkuCategoryGeneralPurpose      = "general_purpose"      // 通用型
	SkuCategoryBurstable           = "burstable"            // 突发性能型
	SkuCategoryComputeOptimized    = "compute_optimized"    // 计算优化型
	SkuCategoryMemoryOptimized     = "memory_optimized"     // 内存优化型
	SkuCategoryStorageIOOptimized  = "storage_optimized"    // 存储IO优化型
	SkuCategoryHardwareAccelerated = "hardware_accelerated" // 硬件加速型
	SkuCategoryHighStorage         = "high_storage"         // 高存储型
	SkuCategoryHighMemory          = "high_memory"          // 高内存型
)

type SServerSkuManager struct {
	db.SStandaloneResourceBaseManager
}

var ServerSkuManager *SServerSkuManager

func init() {
	ServerSkuManager = &SServerSkuManager{
		SStandaloneResourceBaseManager: db.NewStandaloneResourceBaseManager(
			SServerSku{},
			"serverskus_tbl",
			"serversku",
			"serverskus",
		),
	}
	ServerSkuManager.NameRequireAscii = false
}

// SServerSku 实际对应的是instance type清单. 这里的Sku实际指的是instance type。
type SServerSku struct {
	db.SStandaloneResourceBase

	// SkuId       string `width:"64" charset:"ascii" nullable:"false" list:"user" create:"admin_required"`                 // x2.large
	InstanceTypeFamily   string `width:"32" charset:"ascii" nullable:"false" list:"user" create:"admin_optional" update:"admin"` // x2
	InstanceTypeCategory string `width:"32" charset:"utf8" nullable:"false" list:"user" create:"admin_optional" update:"admin"`  // 通用型

	CpuCoreCount int `nullable:"false" list:"user" create:"admin_required" update:"admin"`
	MemorySizeMB int `nullable:"false" list:"user" create:"admin_required" update:"admin"`

	OsName string `width:"32" charset:"ascii" nullable:"false" list:"user" create:"admin_required" update:"admin" default:"Any"` // Windows|Linux|Any

	SysDiskResizable bool   `default:"true" nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	SysDiskType      string `width:"32" charset:"ascii" nullable:"false" list:"user" create:"admin_required" update:"admin"`
	SysDiskMinSizeGB int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"` // not required。 windows比较新的版本都是50G左右。
	SysDiskMaxSizeGB int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"` // not required

	AttachedDiskType   string `nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	AttachedDiskSizeGB int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	AttachedDiskCount  int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"`

	DataDiskTypes    string `width:"128" charset:"ascii" nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	DataDiskMaxCount int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"`

	NicType     string `nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	NicMaxCount int    `default:"1" nullable:"false" list:"user" create:"admin_optional" update:"admin"`

	GpuAttachable bool   `default:"true" nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	GpuSpec       string `width:"128" charset:"ascii" nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	GpuCount      int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	GpuMaxCount   int    `nullable:"false" list:"user" create:"admin_optional" update:"admin"`

	CloudregionId string `width:"128" charset:"ascii" nullable:"false" list:"user" create:"admin_required" update:"admin"`
	ZoneId        string `width:"128" charset:"ascii" nullable:"false" list:"user" create:"admin_optional" update:"admin"`
	Provider      string `width:"64" charset:"ascii" nullable:"true" list:"user" create:"admin_optional" update:"admin"`
}

func inWhiteList(provider string) bool {
	// 只有为true的hypervisor才进行创建和更新操作
	if len(provider) == 0 {
		return true
	}
	switch provider {
	case HYPERVISOR_ESXI, HYPERVISOR_KVM:
		return true
	default:
		return false
	}
}

func (self *SServerSkuManager) AllowListItems(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) bool {
	return true
}

func (self *SServerSku) AllowGetDetails(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) bool {
	return true
}

func (manager *SServerSkuManager) AllowCreateItem(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) bool {
	return db.IsAdminAllowCreate(userCred, manager)
}

func (self *SServerSkuManager) ValidateCreateData(ctx context.Context,
	userCred mcclient.TokenCredential,
	ownerProjId string,
	query jsonutils.JSONObject,
	data *jsonutils.JSONDict,
) (*jsonutils.JSONDict, error) {

	provider, _ := data.GetString("provider")

	if !inWhiteList(provider) {
		return nil, httperrors.NewForbiddenError("can not create instance_type for public cloud %s", provider)
	}

	regionStr := jsonutils.GetAnyString(data, []string{"region", "region_id", "cloudregion", "cloudregion_id"})
	if len(regionStr) > 0 {
		regionObj, err := CloudregionManager.FetchByIdOrName(userCred, regionStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, httperrors.NewResourceNotFoundError("region %s not found", regionStr)
			}
			return nil, httperrors.NewGeneralError(err)
		}
		data.Add(jsonutils.NewString(regionObj.GetId()), "cloudregion_id")
	} else {
		data.Add(jsonutils.NewString(DEFAULT_REGION_ID), "cloudregion_id")
	}
	zoneStr := jsonutils.GetAnyString(data, []string{"zone", "zone_id"})
	if len(zoneStr) > 0 {
		zoneObj, err := ZoneManager.FetchByIdOrName(userCred, zoneStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, httperrors.NewResourceNotFoundError("zone %s not found", zoneStr)
			}
			return nil, httperrors.NewGeneralError(err)
		}
		data.Add(jsonutils.NewString(zoneObj.GetId()), "zone_id")
	}
	return self.SStandaloneResourceBaseManager.ValidateCreateData(ctx, userCred, ownerProjId, query, data)
}

func (self *SServerSkuManager) FetchByZoneExtId(zoneExtId string, name string) (db.IModel, error) {
	zoneObj, err := ZoneManager.FetchByExternalId(zoneExtId)
	if err != nil {
		return nil, err
	}

	return self.FetchByZoneId(zoneObj.GetId(), name)
}

func (self *SServerSkuManager) FetchByZoneId(zoneId string, name string) (db.IModel, error) {
	q := self.Query().Equals("zone_id", zoneId).Equals("name", name)
	count := q.Count()
	if count == 1 {
		obj, err := db.NewModelObject(self)
		if err != nil {
			return nil, err
		}
		err = q.First(obj)
		if err != nil {
			return nil, err
		} else {
			return obj.(db.IStandaloneModel), nil
		}
	} else if count > 1 {
		return nil, sqlchemy.ErrDuplicateEntry
	} else {
		return nil, sql.ErrNoRows
	}
}

func (self *SServerSkuManager) AllowGetPropertyInstanceSpecs(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) bool {
	return true
}

func (self *SServerSkuManager) GetPropertyInstanceSpecs(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject) (jsonutils.JSONObject, error) {
	q := self.Query()
	zone, err := query.GetString("zone")
	if err == nil && len(zone) > 0 {
		q = q.Equals("zone_id", zone)
	} else {
		return nil, httperrors.NewMissingParameterError("zone")
	}

	skus := make([]SServerSku, 0)
	q = q.GroupBy(q.Field("cpu_core_count"), q.Field("memory_size_mb"))
	q = q.Asc(q.Field("cpu_core_count"), q.Field("memory_size_mb"))
	err = q.All(&skus)
	if err != nil {
		log.Errorf("%s", err)
		return nil, httperrors.NewBadRequestError("instance specs list query error")
	}

	cpus := jsonutils.NewArray()
	mems_mb := jsonutils.NewArray()
	cpu_mems_mb := map[int][]int{}

	oc, om := 0, 0
	for i := range skus {
		nc := skus[i].CpuCoreCount
		nm := skus[i].MemorySizeMB

		if nc > oc {
			cpus.Add(jsonutils.NewInt(int64(nc)))
			oc = nc
		}

		if nm > om {
			mems_mb.Add(jsonutils.NewInt(int64(nm)))
			om = nm
		}

		if _, exists := cpu_mems_mb[nc]; !exists {
			cpu_mems_mb[nc] = []int{nm}
		} else {
			cpu_mems_mb[nc] = append(cpu_mems_mb[nc], nm)
		}
	}

	ret := jsonutils.NewDict()
	ret.Add(cpus, "cpus")
	ret.Add(mems_mb, "mems_mb")

	r, err := json.Marshal(&cpu_mems_mb)
	if err != nil {
		log.Errorf("%s", err)
		return nil, httperrors.NewInternalServerError("instance specs list marshal failed")
	}

	r_obj, err := jsonutils.Parse(r)
	if err != nil {
		log.Errorf("%s", err)
		return nil, httperrors.NewInternalServerError("instance specs list parse failed")
	}

	ret.Add(r_obj, "cpu_mems_mb")
	return ret, nil
}

func (self *SServerSku) AllowUpdateItem(ctx context.Context, userCred mcclient.TokenCredential) bool {
	return inWhiteList(self.Provider) && db.IsAdminAllowUpdate(userCred, self)
}

func (self *SServerSku) ValidateUpdateData(
	ctx context.Context,
	userCred mcclient.TokenCredential,
	query jsonutils.JSONObject,
	data *jsonutils.JSONDict) (*jsonutils.JSONDict, error) {

	if !inWhiteList(self.Provider) {
		return nil, httperrors.NewForbiddenError("can not create instance_type for public cloud %s", self.Provider)
	}

	provider, err := data.GetString("provider")
	if err == nil && !inWhiteList(provider) {
		return nil, httperrors.NewForbiddenError("can not create instance_type for public cloud %s", provider)
	}

	zoneStr := jsonutils.GetAnyString(data, []string{"zone", "zone_id"})
	if len(zoneStr) > 0 {
		zoneObj, err := ZoneManager.FetchByIdOrName(userCred, zoneStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, httperrors.NewResourceNotFoundError("zone %s not found", zoneStr)
			}
			return nil, httperrors.NewGeneralError(err)
		}
		data.Add(jsonutils.NewString(zoneObj.GetId()), "zone_id")
	}
	return self.SStandaloneResourceBase.ValidateUpdateData(ctx, userCred, query, data)
}

func (self *SServerSku) AllowDeleteItem(ctx context.Context, userCred mcclient.TokenCredential, query jsonutils.JSONObject, data jsonutils.JSONObject) bool {
	return inWhiteList(self.Provider) && db.IsAdminAllowDelete(userCred, self)
}

func (self *SServerSku) ValidateDeleteCondition(ctx context.Context) error {
	if !inWhiteList(self.Provider) {
		return httperrors.NewForbiddenError("not allow to delete public cloud instance_type: %s", self.Name)
	}
	count := GuestManager.Query().Equals("instance_type", self.Name).Count()
	if count > 0 {
		return httperrors.NewNotEmptyError("instance_type used by servers")
	}
	return nil
}

func (self *SServerSku) GetZoneExternalId() (string, error) {
	zoneObj, err := ZoneManager.FetchById(self.ZoneId)
	if err != nil {
		return "", err
	}

	zone := zoneObj.(*SZone)
	return zone.GetExternalId(), nil
}

func (manager *SServerSkuManager) ListItemFilter(ctx context.Context, q *sqlchemy.SQuery, userCred mcclient.TokenCredential, query jsonutils.JSONObject) (*sqlchemy.SQuery, error) {
	queryDict := query.(*jsonutils.JSONDict)

	provider := jsonutils.GetAnyString(query, []string{"provider"})
	if len(provider) > 0 {
		if provider != "all" {
			q = q.Equals("provider", provider)
		}

		queryDict.Remove("provider")
	} else {
		q = q.Filter(sqlchemy.OR(
			sqlchemy.IsNull(q.Field("provider")),
			sqlchemy.IsEmpty(q.Field("provider")),
		))
	}

	q, err := manager.SStandaloneResourceBaseManager.ListItemFilter(ctx, q, userCred, query)
	if err != nil {
		return nil, err
	}

	regionStr := jsonutils.GetAnyString(query, []string{"region", "cloudregion", "region_id", "cloudregion_id"})
	if len(regionStr) > 0 {
		regionObj, err := CloudregionManager.FetchByIdOrName(nil, regionStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, httperrors.NewResourceNotFoundError2(CloudregionManager.Keyword(), regionStr)
			}
			return nil, httperrors.NewGeneralError(err)
		}
		q = q.Equals("cloudregion_id", regionObj.GetId())
	}

	zoneStr := jsonutils.GetAnyString(query, []string{"zone", "zone_id"})
	if len(zoneStr) > 0 {
		zoneObj, err := ZoneManager.FetchByIdOrName(nil, zoneStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, httperrors.NewResourceNotFoundError2(ZoneManager.Keyword(), zoneStr)
			}
			return nil, httperrors.NewGeneralError(err)
		}
		q = q.Equals("zone_id", zoneObj.GetId())
	}

	return q, err
}

func (manager *SServerSkuManager) FetchSkuByNameAndHypervisor(name string, hypervisor string, checkConsistency bool) (*SServerSku, error) {
	q := manager.Query()
	q = q.Equals("name", name)
	if len(hypervisor) > 0 {
		switch hypervisor {
		case HYPERVISOR_BAREMETAL, HYPERVISOR_CONTAINER:
			return nil, httperrors.NewNotImplementedError("%s not supported", hypervisor)
		case HYPERVISOR_KVM, HYPERVISOR_ESXI, HYPERVISOR_XEN, HOST_TYPE_HYPERV:
			q = q.Filter(sqlchemy.OR(
				sqlchemy.IsEmpty(q.Field("provider")),
				sqlchemy.IsNull(q.Field("provider")),
				sqlchemy.Equals(q.Field("provider"), hypervisor),
			))
		default:
			q = q.Equals("provider", hypervisor)
		}
	} else {
		q = q.IsEmpty("provider")
	}
	skus := make([]SServerSku, 0)
	err := db.FetchModelObjects(manager, q, &skus)
	if err != nil {
		log.Errorf("fetch sku fail %s", err)
		return nil, err
	}
	if len(skus) == 0 {
		log.Errorf("no sku found for %s %s", name, hypervisor)
		return nil, httperrors.NewResourceNotFoundError2(manager.Keyword(), name)
	}
	if len(skus) == 1 {
		return &skus[0], nil
	}
	if checkConsistency {
		for i := 1; i < len(skus); i += 1 {
			if skus[i].CpuCoreCount != skus[0].CpuCoreCount || skus[i].MemorySizeMB != skus[0].MemorySizeMB {
				log.Errorf("inconsistent sku %s %s", jsonutils.Marshal(&skus[0]), jsonutils.Marshal(&skus[i]))
				return nil, httperrors.NewDuplicateResourceError("duplicate instanceType %s", name)
			}
		}
	}
	return &skus[0], nil
}

func (manager *SServerSkuManager) GetSkuCountByProvider(provider string) int {
	q := manager.Query()
	if len(provider) == 0 {
		q = q.IsNotEmpty("provider")
	} else {
		q = q.Equals("provider", provider)
	}

	return q.Count()
}