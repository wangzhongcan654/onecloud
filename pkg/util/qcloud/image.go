package qcloud

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"yunion.io/x/jsonutils"
	"yunion.io/x/log"
	"yunion.io/x/onecloud/pkg/cloudprovider"
	"yunion.io/x/pkg/utils"
)

type ImageStatusType string

const (
	ImageStatusCreating     ImageStatusType = "Creating"
	ImageStatusAvailable    ImageStatusType = "NORMAL"
	ImageStatusUnAvailable  ImageStatusType = "UnAvailable"
	ImageStatusCreateFailed ImageStatusType = "CreateFailed"
)

type SImage struct {
	storageCache *SStoragecache

	ImageId            string          //	镜像ID
	OsName             string          //	镜像操作系统
	ImageType          string          //	镜像类型
	CreatedTime        time.Time       //	镜像创建时间
	ImageName          string          //	镜像名称
	ImageDescription   string          //	镜像描述
	ImageSize          int             //	镜像大小
	Architecture       string          //	镜像架构
	ImageState         ImageStatusType //	镜像状态
	Platform           string          //	镜像来源平台
	ImageCreator       string          //	镜像创建者
	ImageSource        string          //	镜像来源
	SyncPercent        int             //	同步百分比
	IsSupportCloudinit bool            //	镜像是否支持cloud-init
}

func (self *SRegion) GetImages(status string, owner string, imageIds []string, name string, offset int, limit int) ([]SImage, int, error) {
	if limit > 50 || limit <= 0 {
		limit = 50
	}
	params := make(map[string]string)
	params["Limit"] = fmt.Sprintf("%d", limit)
	params["Offset"] = fmt.Sprintf("%d", offset)

	filter := 0
	if len(status) > 0 {
		params[fmt.Sprintf("Filters.%d.Name", filter)] = "image-state"
		params[fmt.Sprintf("Filters.%d.Values.0", filter)] = status
		filter++
	}
	if imageIds != nil && len(imageIds) > 0 {
		for index, imageId := range imageIds {
			params[fmt.Sprintf("ImageIds.%d", index)] = imageId
		}
	}
	if len(owner) > 0 {
		params[fmt.Sprintf("Filters.%d.Name", filter)] = "image-type"
		params[fmt.Sprintf("Filters.%d.Values.0", filter)] = owner
		filter++
	}

	if len(name) > 0 {
		params[fmt.Sprintf("Filters.%d.Name", filter)] = "image-name"
		params[fmt.Sprintf("Filters.%d.Values.0", filter)] = name
		filter++
	}

	images := make([]SImage, 0)
	body, err := self.cvmRequest("DescribeImages", params)
	if err != nil {
		return nil, 0, err
	}
	err = body.Unmarshal(&images, "ImageSet")
	if err != nil {
		return nil, 0, err
	}
	for i := 0; i < len(images); i++ {
		images[i].storageCache = self.getStoragecache()
	}
	total, _ := body.Float("TotalCount")
	return images, int(total), nil
}
func (self *SImage) GetMetadata() *jsonutils.JSONDict {
	return nil
}

func (self *SImage) GetId() string {
	return self.ImageId
}

func (self *SImage) GetName() string {
	return self.ImageName
}

func (self *SImage) IsEmulated() bool {
	return false
}

func (self *SImage) GetGlobalId() string {
	return fmt.Sprintf("%s-%s")
}

func (self *SImage) Delete() error {
	return self.storageCache.region.DeleteImage(self.ImageId)
}

func (self *SImage) GetStatus() string {
	return string(self.ImageState)
}

func (self *SImage) Refresh() error {
	new, err := self.storageCache.region.GetImage(self.ImageId)
	if err != nil {
		return err
	}
	return jsonutils.Update(self, new)
}

func (self *SRegion) GetImage(imageId string) (*SImage, error) {
	images, _, err := self.GetImages("", "", []string{imageId}, "", 0, 1)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("image %s not found", imageId)
	}
	return &images[0], nil
}

func (self *SImage) GetIStoragecache() cloudprovider.ICloudStoragecache {
	return self.storageCache
}

func (self *SRegion) DeleteImage(imageId string) error {
	params := make(map[string]string)
	params["ImageIds.0"] = imageId

	_, err := self.cvmRequest("DeleteImages", params)
	return err
}

func (self *SRegion) GetImageStatus(imageId string) (ImageStatusType, error) {
	image, err := self.GetImage(imageId)
	if err != nil {
		return "", err
	}
	return image.ImageState, nil
}

func (self *SRegion) GetImageByName(name string) (*SImage, error) {
	images, _, err := self.GetImages("", "", nil, name, 0, 1)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, cloudprovider.ErrNotFound
	}
	return &images[0], nil
}

type ImportImageOsListSupported struct {
	Linux   []string //"CentOS|Ubuntu|Debian|OpenSUSE|SUSE|CoreOS|FreeBSD|Other Linux"
	Windows []string //"Windows Server 2008|Windows Server 2012|Windows Server 2016"
}

type ImportImageOsVersionSet struct {
	Architecture []string
	OsName       string //"CentOS|Ubuntu|Debian|OpenSUSE|SUSE|CoreOS|FreeBSD|Other Linux|Windows Server 2008|Windows Server 2012|Windows Server 2016"
	OsVersions   []string
}

type SupportImageSet struct {
	ImportImageOsListSupported ImportImageOsListSupported
	ImportImageOsVersionSet    []ImportImageOsVersionSet
}

func (self *SRegion) GetSupportImageSet() (*SupportImageSet, error) {
	body, err := self.cvmRequest("DescribeImportImageOs", map[string]string{})
	if err != nil {
		return nil, err
	}
	imageSet := SupportImageSet{}
	return &imageSet, body.Unmarshal(&imageSet)
}

func (self *SRegion) GetImportImageParams(name string, osArch, osDist, osVersion string, imageUrl string) (map[string]string, error) {
	params := map[string]string{}
	imageSet, err := self.GetSupportImageSet()
	if err != nil {
		return nil, err
	}
	osType := ""
	for _, _imageSet := range imageSet.ImportImageOsVersionSet {
		if strings.ToLower(osDist) == strings.ToLower(_imageSet.OsName) { //Linux一般可正常匹配
			osType = _imageSet.OsName
		} else if strings.Contains(strings.ToLower(_imageSet.OsName), "windows") && strings.Contains(strings.ToLower(osDist), "windows") {
			info := strings.Split(_imageSet.OsName, " ")
			_osVersion := "2008"
			for _, version := range info {
				if _, err := strconv.Atoi(version); err == nil {
					_osVersion = version
					break
				}
			}
			if strings.Contains(osDist+osVersion, _osVersion) {
				osType = _imageSet.OsName
			}
		}
		if len(osType) == 0 {
			continue
		}
		if !utils.IsInStringArray(osArch, _imageSet.Architecture) {
			osArch = "x86_64"
		}
		for _, _osVersion := range _imageSet.OsVersions {
			if strings.HasPrefix(osVersion, _osVersion) {
				osVersion = _osVersion
				break
			}
		}
		if !utils.IsInStringArray(osVersion, _imageSet.OsVersions) {
			osVersion = "-"
			if len(_imageSet.OsVersions) > 0 {
				osVersion = _imageSet.OsVersions[0]
			}
		}
		break
	}
	if len(osType) == 0 {
		osType = "Other Linux"
		osArch = "x86_64"
		osVersion = "-"
	}

	params["ImageName"] = name
	params["OsType"] = osType
	params["OsVersion"] = osVersion
	params["Architecture"] = osArch // "x86_64|i386"
	params["ImageUrl"] = imageUrl
	params["Force"] = "true"
	return params, nil
}

func (self *SRegion) ImportImage(name string, osArch, osDist, osVersion string, imageUrl string) (*SImage, error) {
	params, err := self.GetImportImageParams(name, osArch, osDist, osVersion, imageUrl)
	if err != nil {
		return nil, err
	}

	log.Debugf("Upload image with params %#v", params)

	if _, err := self.cvmRequest("ImportImage", params); err != nil {
		return nil, err
	}
	for i := 0; i < 8; i++ {
		image, err := self.GetImageByName(name)
		if err == nil {
			return image, nil
		}
		time.Sleep(time.Minute * time.Duration(i))
	}
	return nil, cloudprovider.ErrNotFound
}