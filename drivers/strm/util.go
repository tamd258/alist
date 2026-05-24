package strm

import (
	"context"
	"fmt"
	stdpath "path"
	"strings"

	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
)

func (d *Strm) listRoot() []model.Obj {
	var objs []model.Obj
	for k := range d.pathMap {
		obj := model.Object{
			Path:     "/" + k,
			Name:     k,
			IsFolder: true,
			Modified: d.Modified,
		}
		objs = append(objs, &obj)
	}
	return objs
}

// do others that not defined in Driver interface
func getPair(path string) (string, string) {
	if strings.Contains(path, ":") {
		pair := strings.SplitN(path, ":", 2)
		if !strings.Contains(pair[0], "/") {
			return pair[0], pair[1]
		}
	}
	return stdpath.Base(path), path
}

func (d *Strm) getRootAndPath(path string) (string, string) {
	if d.autoFlatten {
		return d.oneKey, path
	}
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func (d *Strm) list(ctx context.Context, dst, sub string, args *fs.ListArgs) ([]model.Obj, error) {
	reqPath := stdpath.Join(dst, sub)
	objs, err := fs.List(ctx, reqPath, args)
	if err != nil {
		return nil, err
	}
	return d.convert2strmObjs(ctx, reqPath, objs), nil
}

func (d *Strm) convert2strmObjs(ctx context.Context, reqPath string, objs []model.Obj) []model.Obj {
	var validObjs []model.Obj
	for _, obj := range objs {
		id, name, path := "", obj.GetName(), ""
		size := int64(0)
		if !obj.IsDir() {
			path = stdpath.Join(reqPath, obj.GetName())
			sourceExt := utils.Ext(name)
			if _, ok := d.downloadSuffix[sourceExt]; ok {
				size = obj.GetSize()
			} else if _, ok := d.supportSuffix[sourceExt]; ok {
				id = "strm"
				name = strings.TrimSuffix(name, "."+sourceExt) + ".strm"
				size = int64(len(d.getLink(ctx, path)))
			} else {
				continue
			}
		}
		objRes := model.Object{
			ID:       id,
			Path:     path,
			Name:     name,
			Size:     size,
			Modified: obj.ModTime(),
			IsFolder: obj.IsDir(),
		}
		thumb, ok := model.GetThumb(obj)
		if !ok {
			validObjs = append(validObjs, &objRes)
			continue
		}
		validObjs = append(validObjs, &model.ObjThumb{
			Object: objRes,
			Thumbnail: model.Thumbnail{
				Thumbnail: thumb,
			},
		})
	}
	return validObjs
}

func (d *Strm) getLink(ctx context.Context, path string) string {
	finalPath := path
	if d.EncodePath {
		finalPath = utils.EncodePath(path, true)
	}
	if d.WithSign {
		signPath := sign.Sign(path)
		finalPath = fmt.Sprintf("%s?sign=%s", finalPath, signPath)
	}
	pathPrefix := d.PathPrefix
	if len(pathPrefix) > 0 {
		finalPath = stdpath.Join(pathPrefix, finalPath)
	}
	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	if d.WithoutUrl {
		return finalPath
	}
	apiUrl := d.SiteUrl
	if len(apiUrl) > 0 {
		apiUrl = strings.TrimSuffix(apiUrl, "/")
	} else {
		apiUrl = common.GetApiUrl(common.GetHttpReq(ctx))
	}
	return fmt.Sprintf("%s%s",
		apiUrl,
		finalPath)
}
