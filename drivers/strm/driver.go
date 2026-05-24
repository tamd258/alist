package strm

import (
	"context"
	"errors"
	stdpath "path"
	"strings"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	log "github.com/sirupsen/logrus"
)

// strmFile wraps strings.Reader to implement model.File (io.Reader + io.ReaderAt + io.Seeker + io.Closer)
type strmFile struct {
	*strings.Reader
}

func (s *strmFile) Close() error {
	return nil
}

type Strm struct {
	model.Storage
	Addition
	pathMap     map[string][]string
	autoFlatten bool
	oneKey      string

	supportSuffix  map[string]struct{}
	downloadSuffix map[string]struct{}
}

func (d *Strm) Config() driver.Config {
	return config
}

func (d *Strm) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Strm) Init(ctx context.Context) error {
	if d.Paths == "" {
		return errors.New("paths is required")
	}
	if d.SaveStrmToLocal && len(d.SaveStrmLocalPath) <= 0 {
		return errors.New("SaveStrmLocalPath is required")
	}
	d.pathMap = make(map[string][]string)
	for _, path := range strings.Split(d.Paths, "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		k, v := getPair(path)
		d.pathMap[k] = append(d.pathMap[k], v)
		if d.SaveStrmToLocal {
			err := InsertStrm(utils.FixAndCleanPath(strings.TrimSpace(path)), d)
			if err != nil {
				log.Errorf("insert strmTrie error: %v", err)
				continue
			}
		}
	}
	if len(d.pathMap) == 1 {
		for k := range d.pathMap {
			d.oneKey = k
		}
		d.autoFlatten = true
	} else {
		d.oneKey = ""
		d.autoFlatten = false
	}

	var supportTypes []string
	if d.FilterFileTypes == "" {
		d.FilterFileTypes = "mp4,mkv,flv,avi,wmv,ts,rmvb,webm,mp3,flac,aac,wav,ogg,m4a,wma,alac"
	}
	supportTypes = strings.Split(d.FilterFileTypes, ",")
	d.supportSuffix = map[string]struct{}{}
	for _, ext := range supportTypes {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext != "" {
			d.supportSuffix[ext] = struct{}{}
		}
	}

	var downloadTypes []string
	if d.DownloadFileTypes == "" {
		d.DownloadFileTypes = "ass,srt,vtt,sub,strm"
	}
	downloadTypes = strings.Split(d.DownloadFileTypes, ",")
	d.downloadSuffix = map[string]struct{}{}
	for _, ext := range downloadTypes {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext != "" {
			d.downloadSuffix[ext] = struct{}{}
		}
	}

	if d.Version != 5 {
		for _, ext := range strings.Split("mp4,mkv,flv,avi,wmv,ts,rmvb,webm,mp3,flac,aac,wav,ogg,m4a,wma,alac", ",") {
			if _, ok := d.supportSuffix[ext]; !ok {
				d.supportSuffix[ext] = struct{}{}
				supportTypes = append(supportTypes, ext)
			}
		}
		d.FilterFileTypes = strings.Join(supportTypes, ",")

		for _, ext := range strings.Split("ass,srt,vtt,sub,strm", ",") {
			if _, ok := d.downloadSuffix[ext]; !ok {
				d.downloadSuffix[ext] = struct{}{}
				downloadTypes = append(downloadTypes, ext)
			}
		}
		d.DownloadFileTypes = strings.Join(downloadTypes, ",")
		d.PathPrefix = "/d"
		d.Version = 5
	}
	if len(d.SaveLocalMode) == 0 {
		d.SaveLocalMode = SaveLocalInsertMode
	}
	return nil
}

func (d *Strm) Drop(ctx context.Context) error {
	d.pathMap = nil
	d.downloadSuffix = nil
	d.supportSuffix = nil
	for _, path := range strings.Split(d.Paths, "\n") {
		RemoveStrm(utils.FixAndCleanPath(strings.TrimSpace(path)), d)
	}
	return nil
}

func (Addition) GetRootPath() string {
	return "/"
}

func (d *Strm) Get(ctx context.Context, path string) (model.Obj, error) {
	root, sub := d.getRootAndPath(path)
	dsts, ok := d.pathMap[root]
	if !ok {
		return nil, errs.ObjectNotFound
	}
	for _, dst := range dsts {
		reqPath := stdpath.Join(dst, sub)
		obj, err := fs.Get(ctx, reqPath, &fs.GetArgs{NoLog: true})
		if err != nil {
			continue
		}
		// fs.Get 没报错，说明不是strm驱动映射的路径，需要直接返回
		size := int64(0)
		if !obj.IsDir() {
			size = obj.GetSize()
			path = reqPath // 把路径设置为真实的，供Link直接读取
		}
		return &model.Object{
			Path:     path,
			Name:     obj.GetName(),
			Size:     size,
			Modified: obj.ModTime(),
			IsFolder: obj.IsDir(),
			HashInfo: obj.GetHash(),
		}, nil
	}
	if strings.HasSuffix(path, ".strm") {
		// 上面fs.Get都没找到且后缀为.strm
		// 返回errs.NotSupport使得op.Get尝试从op.List中查找
		return nil, errs.NotSupport
	}
	return nil, errs.ObjectNotFound
}

func (d *Strm) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	path := dir.GetPath()
	if utils.PathEqual(path, "/") && !d.autoFlatten {
		return d.listRoot(), nil
	}
	root, sub := d.getRootAndPath(path)
	dsts, ok := d.pathMap[root]
	if !ok {
		return nil, errs.ObjectNotFound
	}
	var objs []model.Obj
	fsArgs := &fs.ListArgs{NoLog: true, Refresh: args.Refresh}
	for _, dst := range dsts {
		tmp, err := d.list(ctx, dst, sub, fsArgs)
		if err == nil {
			objs = append(objs, tmp...)
		}
	}
	return objs, nil
}

func (d *Strm) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if file.GetID() == "strm" {
		link := d.getLink(ctx, file.GetPath())
		return &model.Link{
			MFile: &strmFile{Reader: strings.NewReader(link)},
		}, nil
	}
	reqPath := file.GetPath()
	storage, reqActualPath, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		return nil, err
	}
	link, _, err := op.Link(ctx, storage, reqActualPath, args)
	return link, err
}

var _ driver.Driver = (*Strm)(nil)
