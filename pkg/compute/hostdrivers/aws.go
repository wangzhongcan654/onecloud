package hostdrivers

import (
	"context"
	"fmt"

	"yunion.io/x/log"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/lockman"
	"yunion.io/x/onecloud/pkg/cloudprovider"
	"yunion.io/x/onecloud/pkg/compute/options"
	"yunion.io/x/onecloud/pkg/httperrors"
	"yunion.io/x/pkg/utils"

	"yunion.io/x/jsonutils"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/compute/models"
)

type SAwsHostDriver struct {
	SBaseHostDriver
}

func (self *SAwsHostDriver) GetHostType() string {
	return models.HOST_TYPE_AWS
}

func (self *SAwsHostDriver) ValidateDiskSize(storage *models.SStorage, sizeGb int) error {
	if storage.StorageType == models.STORAGE_GP2_SSD {
		if sizeGb < 1 || sizeGb > 16384 {
			return fmt.Errorf("The %s disk size must be in the range of 1G ~ 16384GB", storage.StorageType)
		}
	} else if storage.StorageType == models.STORAGE_IO1_SSD {
		if sizeGb < 4 || sizeGb > 16384 {
			return fmt.Errorf("The %s disk size must be in the range of 4G ~ 16384GB", storage.StorageType)
		}
	} else if utils.IsInStringArray(storage.StorageType, []string{models.STORAGE_ST1_HDD, models.STORAGE_SC1_SSD}) {
		if sizeGb < 500 || sizeGb > 16384 {
			return fmt.Errorf("The %s disk size must be in the range of 500G ~ 16384GB", storage.StorageType)
		}
	} else if storage.StorageType == models.STORAGE_STANDARD_SSD {
		if sizeGb < 1 || sizeGb > 1024 {
			return fmt.Errorf("The %s disk size must be in the range of 1G ~ 1024GB", storage.StorageType)
		}
	} else {
		return fmt.Errorf("Not support create %s disk", storage.StorageType)
	}
	return nil
}

func (self *SAwsHostDriver) CheckAndSetCacheImage(ctx context.Context, host *models.SHost, storageCache *models.SStoragecache, task taskman.ITask) error {
	params := task.GetParams()
	imageId, err := params.GetString("image_id")
	if err != nil {
		return err
	}

	osArch, _ := params.GetString("os_arch")
	osType, _ := params.GetString("os_type")
	osDist, _ := params.GetString("os_distribution")
	osVersion, _ := params.GetString("os_version")

	isForce := jsonutils.QueryBoolean(params, "is_force", false)
	userCred := task.GetUserCred()
	taskman.LocalTaskRun(task, func() (jsonutils.JSONObject, error) {

		lockman.LockRawObject(ctx, "cachedimages", fmt.Sprintf("%s-%s", storageCache.Id, imageId))
		defer lockman.ReleaseRawObject(ctx, "cachedimages", fmt.Sprintf("%s-%s", storageCache.Id, imageId))

		scimg := models.StoragecachedimageManager.Register(ctx, task.GetUserCred(), storageCache.Id, imageId)

		iStorageCache, err := storageCache.GetIStorageCache()
		if err != nil {
			return nil, err
		}

		extImgId, err := iStorageCache.UploadImage(userCred, imageId, osArch, osType, osDist, osVersion, scimg.ExternalId, isForce)

		if err != nil {
			return nil, err
		} else {
			scimg.SetExternalId(extImgId)

			ret := jsonutils.NewDict()
			ret.Add(jsonutils.NewString(extImgId), "image_id")
			return ret, nil
		}
	})
	return nil
}

func (self *SAwsHostDriver) RequestPrepareSaveDiskOnHost(ctx context.Context, host *models.SHost, disk *models.SDisk, imageId string, task taskman.ITask) error {
	task.ScheduleRun(nil)
	return nil
}

func (self *SAwsHostDriver) RequestSaveUploadImageOnHost(ctx context.Context, host *models.SHost, disk *models.SDisk, imageId string, task taskman.ITask, data jsonutils.JSONObject) error {
	if iDisk, err := disk.GetIDisk(); err != nil {
		return err
	} else if iStorage, err := disk.GetIStorage(); err != nil {
		return err
	} else if iStoragecache := iStorage.GetIStoragecache(); iStoragecache == nil {
		return httperrors.NewResourceNotFoundError("fail to find iStoragecache for storage: %s", iStorage.GetName())
	} else {
		taskman.LocalTaskRun(task, func() (jsonutils.JSONObject, error) {
			if snapshot, err := iDisk.CreateISnapshot(fmt.Sprintf("Snapshot-%s", imageId), "PrepareSaveImage"); err != nil {
				return nil, err
			} else {
				params := task.GetParams()
				osType, _ := params.GetString("properties", "os_type")

				scimg := models.StoragecachedimageManager.Register(ctx, task.GetUserCred(), iStoragecache.GetId(), imageId)
				if scimg.Status != models.CACHED_IMAGE_STATUS_READY {
					scimg.SetStatus(task.GetUserCred(), models.CACHED_IMAGE_STATUS_CACHING, "request_prepare_save_disk_on_host")
				}
				if iImage, err := iStoragecache.CreateIImage(snapshot.GetId(), fmt.Sprintf("Image-%s", imageId), osType, ""); err != nil {
					log.Errorf("fail to create iImage: %v", err)
					scimg.SetStatus(task.GetUserCred(), models.CACHED_IMAGE_STATUS_CACHE_FAILED, err.Error())
					return nil, err
				} else {
					scimg.SetExternalId(iImage.GetId())
					if result, err := iStoragecache.DownloadImage(task.GetUserCred(), imageId, iImage.GetId(), options.Options.TempPath); err != nil {
						scimg.SetStatus(task.GetUserCred(), models.CACHED_IMAGE_STATUS_CACHE_FAILED, err.Error())
						return nil, err
					} else {
						if err := iImage.Delete(); err != nil {
							log.Errorf("Delete iImage %s failed: %v", iImage.GetId(), err)
						}
						if err := snapshot.Delete(); err != nil {
							log.Errorf("Delete snapshot %s failed: %v", snapshot.GetId(), err)
						}
						scimg.SetStatus(task.GetUserCred(), models.CACHED_IMAGE_STATUS_READY, "")
						return result, nil
					}
				}
			}
		})
	}
	return nil
}

func (self *SAwsHostDriver) RequestAllocateDiskOnStorage(ctx context.Context, host *models.SHost, storage *models.SStorage, disk *models.SDisk, task taskman.ITask, content *jsonutils.JSONDict) error {
	if iCloudStorage, err := storage.GetIStorage(); err != nil {
		return err
	} else {
		if size, err := content.Int("size"); err != nil {
			return err
		} else {
			size = size >> 10
			if iDisk, err := iCloudStorage.CreateIDisk(disk.GetName(), int(size), ""); err != nil {
				return err
			} else {
				if _, err := disk.GetModelManager().TableSpec().Update(disk, func() error {
					disk.ExternalId = iDisk.GetGlobalId()

					if metaData := iDisk.GetMetadata(); metaData != nil {
						meta := make(map[string]string)
						if err := metaData.Unmarshal(meta); err != nil {
							log.Errorf("Get disk %s Metadata error: %v", disk.Name, err)
						} else {
							for key, value := range meta {
								if err := disk.SetMetadata(ctx, key, value, task.GetUserCred()); err != nil {
									log.Errorf("set disk %s mata %s => %s error: %v", disk.Name, key, value, err)
								}
							}
						}
					}

					return nil
				}); err != nil {
					log.Errorf("Update disk externalId err: %v", err)
					return err
				}
				data := jsonutils.NewDict()
				data.Add(jsonutils.NewInt(int64(iDisk.GetDiskSizeMB())), "disk_size")
				data.Add(jsonutils.NewString(iDisk.GetDiskFormat()), "disk_format")
				task.ScheduleRun(data)
			}
		}
	}
	return nil
}

func (self *SAwsHostDriver) RequestDeallocateDiskOnHost(host *models.SHost, storage *models.SStorage, disk *models.SDisk, task taskman.ITask) error {
	data := jsonutils.NewDict()
	if iCloudStorage, err := storage.GetIStorage(); err != nil {
		return err
	} else if iDisk, err := iCloudStorage.GetIDisk(disk.GetExternalId()); err != nil {
		if err == cloudprovider.ErrNotFound {
			task.ScheduleRun(data)
			return nil
		}
		return err
	} else if err := iDisk.Delete(); err != nil {
		return err
	}
	task.ScheduleRun(data)
	return nil
}

func (self *SAwsHostDriver) RequestResizeDiskOnHostOnline(host *models.SHost, storage *models.SStorage, disk *models.SDisk, size int64, task taskman.ITask) error {
	return self.RequestResizeDiskOnHost(host, storage, disk, size, task)
}

func (self *SAwsHostDriver) RequestResizeDiskOnHost(host *models.SHost, storage *models.SStorage, disk *models.SDisk, size int64, task taskman.ITask) error {
	if iCloudStorage, err := storage.GetIStorage(); err != nil {
		return err
	} else if iDisk, err := iCloudStorage.GetIDisk(disk.GetExternalId()); err != nil {
		return err
	} else if err := iDisk.Resize(size >> 10); err != nil {
		return err
	} else {
		task.ScheduleRun(jsonutils.Marshal(map[string]int64{"disk_size": size}))
	}
	return nil
}

func init() {
	driver := SAwsHostDriver{}
	models.RegisterHostDriver(&driver)
}