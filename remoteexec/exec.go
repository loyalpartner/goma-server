// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	"go.chromium.org/goma/server/command/descriptor"
	"go.chromium.org/goma/server/command/descriptor/posixpath"
	"go.chromium.org/goma/server/command/descriptor/winpath"
	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	cmdpb "go.chromium.org/goma/server/proto/command"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/remoteexec/merkletree"
	"go.chromium.org/goma/server/rpc"
)

type request struct {
	f         *Adapter
	userGroup string
	gomaReq   *gomapb.ExecReq
	gomaResp  *gomapb.ExecResp

	client Client
	cas    *cas.CAS

	cmdConfig *cmdpb.Config
	cmdFiles  []*cmdpb.FileSpec

	digestStore *digest.Store
	tree        *merkletree.MerkleTree
	input       gomaInputInterface

	filepath clientFilePath

	args         []string
	envs         []string
	outputs      []string
	outputDirs   []string
	platform     *rpb.Platform
	action       *rpb.Action
	actionDigest *rpb.Digest

	allowChroot bool
	needChroot  bool

	crossTarget string

	err error
}

func (r *request) Close() {
	r.input.Close()
}

type clientFilePath interface {
	IsAbs(path string) bool
	Base(path string) string
	Dir(path string) string
	Join(elem ...string) string
	Rel(basepath, targpath string) (string, error)
	Clean(path string) string
	SplitElem(path string) []string
	PathSep() string
}

func doNotCache(req *gomapb.ExecReq) bool {
	switch req.GetCachePolicy() {
	case gomapb.ExecReq_LOOKUP_AND_STORE, gomapb.ExecReq_STORE_ONLY, gomapb.ExecReq_LOOKUP_AND_STORE_SUCCESS:
		return false
	default:
		return true
	}
}

func skipCacheLookup(req *gomapb.ExecReq) bool {
	switch req.GetCachePolicy() {
	case gomapb.ExecReq_STORE_ONLY:
		return true
	default:
		return false
	}
}

// Takes an input of environment flag defs, e.g. FLAG_NAME=value, and returns an array of
// rpb.Command_EnvironmentVariable with these flag names and values.
func createEnvVars(ctx context.Context, envs []string) []*rpb.Command_EnvironmentVariable {
	envMap := make(map[string]string)
	logger := log.FromContext(ctx)
	for _, env := range envs {
		e := strings.SplitN(env, "=", 2)
		key, value := e[0], e[1]
		storedValue, ok := envMap[key]
		if ok {
			logger.Infof("Duplicate env var: %s=%s => %s", key, storedValue, value)
		}
		envMap[key] = value
	}

	// EnvironmentVariables must be lexicographically sorted by name.
	var envKeys []string
	for k := range envMap {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)

	var envVars []*rpb.Command_EnvironmentVariable
	for _, k := range envKeys {
		envVars = append(envVars, &rpb.Command_EnvironmentVariable{
			Name:  k,
			Value: envMap[k],
		})
	}
	return envVars
}

// ID returns compiler proxy id of the request.
func (r *request) ID() string {
	if r == nil {
		return "<unknown>"
	}
	return r.gomaReq.GetRequesterInfo().GetCompilerProxyId()
}

// Err returns error of the request.
func (r *request) Err() error {
	switch status.Code(r.err) {
	case codes.OK:
		return nil
	case codes.Canceled, codes.DeadlineExceeded, codes.Aborted:
		// report cancel/deadline exceeded/aborted as is
		return r.err

	case codes.Unauthenticated:
		// unauthenticated happens when oauth2 access token
		// is expired during exec call.
		// e.g.
		// desc = Request had invalid authentication credentials.
		//   Expected OAuth 2 access token, login cookie or
		//   other valid authentication credential.
		//   See https://developers.google.com/identity/sign-in/web/devconsole-project.
		// report it back to caller, so caller could retry it
		// again with new refreshed oauth2 access token.
		return r.err
	default:
		return status.Errorf(codes.Internal, "exec error: %v", r.err)
	}
}

func (r *request) instanceName() string {
	basename := r.cmdConfig.GetRemoteexecPlatform().GetRbeInstanceBasename()
	if basename == "" {
		return r.f.Instance()
	}
	return path.Join(r.f.InstancePrefix, basename)
}

// getInventoryData looks up Config and FileSpec from Inventory, and creates
// execution platform properties from Config.
// It returns non-nil ExecResp for:
// - compiler/subprogram not found
// - bad path_type in command config
func (r *request) getInventoryData(ctx context.Context) *gomapb.ExecResp {
	if r.err != nil {
		return nil
	}

	logger := log.FromContext(ctx)

	cmdConfig, cmdFiles, err := r.f.Inventory.Pick(ctx, r.gomaReq, r.gomaResp)
	if err != nil {
		logger.Errorf("Inventory.Pick failed: %v", err)
		return r.gomaResp
	}

	r.filepath, err = descriptor.FilePathOf(cmdConfig.GetCmdDescriptor().GetSetup().GetPathType())
	if err != nil {
		logger.Errorf("bad path type in setup %s: %v", cmdConfig.GetCmdDescriptor().GetSelector(), err)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("bad compiler config: %v", err))
		return r.gomaResp
	}
	if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
		r.filepath = winpath.FilePath{}
		// drop .bat suffix
		// http://b/185210502#comment12
		cmdFiles[0].Path = strings.TrimSuffix(cmdFiles[0].Path, ".bat")
	}

	r.cmdConfig = cmdConfig
	r.cmdFiles = cmdFiles

	r.platform = &rpb.Platform{}
	for _, prop := range cmdConfig.GetRemoteexecPlatform().GetProperties() {
		r.addPlatformProperty(ctx, prop.Name, prop.Value)
	}
	if len(r.gomaReq.GetRequesterInfo().GetPlatformProperties()) > 0 {
		for _, pp := range r.gomaReq.GetRequesterInfo().GetPlatformProperties() {
			if !isSafePlatformProperty(pp.GetName(), pp.GetValue()) {
				logger.Errorf("unsafe user platform property: %v", pp)
				r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
				r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("unsafe platform property: %v", pp))
				continue
			}
			logger.Infof("override by user platform property: %v", pp)
			r.addPlatformProperty(ctx, pp.GetName(), pp.GetValue())
		}
		if len(r.gomaResp.ErrorMessage) > 0 {
			return r.gomaResp
		}
	}
	r.allowChroot = cmdConfig.GetRemoteexecPlatform().GetHasNsjail()
	logger.Infof("platform: %s, allowChroot=%t path_tpye=%s windows_cross=%t", r.platform, r.allowChroot, cmdConfig.GetCmdDescriptor().GetSetup().GetPathType(), cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross())
	return nil
}

func isSafePlatformProperty(name, value string) bool {
	switch name {
	case "container-image", "InputRootAbsolutePath", "cache-silo":
		return true
	case "dockerRuntime":
		return value == "runsc"
	}
	return false
}

func (r *request) addPlatformProperty(ctx context.Context, name, value string) {
	for _, p := range r.platform.Properties {
		if p.Name == name {
			p.Value = value
			return
		}
	}
	r.platform.Properties = append(r.platform.Properties, &rpb.Platform_Property{
		Name:  name,
		Value: value,
	})
}

type inputDigestData struct {
	filename string
	digest.Data
}

func (id inputDigestData) String() string {
	return fmt.Sprintf("%s %s", id.Data.String(), id.filename)
}

func changeSymlinkAbsToRel(e merkletree.Entry) (merkletree.Entry, error) {
	dir := filepath.Dir(e.Name)
	if !filepath.IsAbs(dir) {
		return merkletree.Entry{}, fmt.Errorf("absolute symlink path not allowed: %s -> %s", e.Name, e.Target)
	}
	target, err := filepath.Rel(dir, e.Target)
	if err != nil {
		return merkletree.Entry{}, fmt.Errorf("failed to make relative for absolute symlink path: %s in %s -> %s: %v", e.Name, dir, e.Target, err)
	}
	e.Target = target
	return e, nil
}

type gomaInputInterface interface {
	toDigest(context.Context, *gomapb.ExecReq_Input) (digest.Data, error)
	upload(context.Context, []*gomapb.FileBlob) ([]string, error)
	Close()
}

func uploadInputFiles(ctx context.Context, inputs []*gomapb.ExecReq_Input, gi gomaInputInterface) error {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.request.uploadInputFiles")
	defer span.End()
	span.AddAttributes(trace.Int64Attribute("uploads", int64(len(inputs))))
	count := 0
	size := 0
	batchLimit := 500
	sizeLimit := 10 * 1024 * 1024

	beginOffset := 0
	hashKeys := make([]string, len(inputs))

	eg, ctx := errgroup.WithContext(ctx)

	for i, input := range inputs {
		count++
		size += len(input.Content.Content)

		// Upload a bunch of file blobs if one of the following:
		// - inputs[uploadBegin:i] reached the upload blob count limit
		// - inputs[uploadBegin:i] exceeds the upload blob size limit
		// - we are on the last blob to be uploaded
		if count < batchLimit && size < sizeLimit && i < len(inputs)-1 {
			continue
		}

		inputs := inputs[beginOffset : i+1]
		results := hashKeys[beginOffset : i+1]
		eg.Go(func() error {
			contents := make([]*gomapb.FileBlob, len(inputs))
			for i, input := range inputs {
				contents[i] = input.Content
			}

			var hks []string
			var err error
			err = rpc.Retry{}.Do(ctx, func() error {
				hks, err = gi.upload(ctx, contents)
				return err
			})

			if err != nil {
				return fmt.Errorf("setup %s input error: %v", inputs[0].GetFilename(), err)
			}
			if len(hks) != len(contents) {
				return fmt.Errorf("invalid number of hash keys: %d, want %d", len(hks), len(contents))
			}
			for i, hk := range hks {
				input := inputs[i]
				if input.GetHashKey() != hk {
					return fmt.Errorf("hashkey missmatch: embedded input %s %s != %s", input.GetFilename(), input.GetHashKey(), hk)
				}
				results[i] = hk
			}
			return nil
		})
		beginOffset = i + 1
		count = 0
		size = 0
	}

	defer func() {
		maxOutputSize := len(inputs)
		if maxOutputSize > 10 {
			maxOutputSize = 10
		}
		successfulUploadsMsg := make([]string, 0, maxOutputSize+1)
		for i, input := range inputs {
			if len(hashKeys[i]) == 0 {
				continue
			}
			if i == maxOutputSize && i < len(inputs)-1 {
				successfulUploadsMsg = append(successfulUploadsMsg, "...")
				break
			}
			successfulUploadsMsg = append(successfulUploadsMsg, fmt.Sprintf("%s -> %s", input.GetFilename(), hashKeys[i]))
		}
		logger := log.FromContext(ctx)
		logger.Infof("embedded inputs: %v", successfulUploadsMsg)

		numSuccessfulUploads := 0
		for _, hk := range hashKeys {
			if len(hk) > 0 {
				numSuccessfulUploads++
			}
		}
		if numSuccessfulUploads < len(inputs) {
			logger.Errorf("%d file blobs successfully uploaded, out of %d", numSuccessfulUploads, len(inputs))
		}
	}()

	return eg.Wait()
}

func dedupInputs(filepath clientFilePath, cwd string, inputs []*gomapb.ExecReq_Input) []*gomapb.ExecReq_Input {
	var deduped []*gomapb.ExecReq_Input
	m := make(map[string]int) // key name -> index in deduped

	for _, input := range inputs {
		fname := input.GetFilename()
		if !filepath.IsAbs(fname) {
			fname = filepath.Join(cwd, fname)
		}
		k := strings.ToLower(fname)
		i, found := m[k]
		if !found {
			m[k] = len(deduped)
			deduped = append(deduped, input)
			continue
		}
		// If there is already registered filename, compare and take shorter one.
		if len(input.GetFilename()) < len(deduped[i].GetFilename()) {
			deduped[i] = input
			continue
		}
		// If length is same, take lexicographically smaller one.
		if len(input.GetFilename()) == len(deduped[i].GetFilename()) && input.GetFilename() < deduped[i].GetFilename() {
			deduped[i] = input
		}
	}
	return deduped
}

type inputFileResult struct {
	missingInput  string
	missingReason string
	file          merkletree.Entry
	needUpload    bool
	err           error
}

func inputFiles(ctx context.Context, inputs []*gomapb.ExecReq_Input, gi gomaInputInterface, rootRel func(string) (string, error), executableInputs map[string]bool) []inputFileResult {
	logger := log.FromContext(ctx)
	var wg sync.WaitGroup
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.request.inputFiles")
	defer span.End()
	span.AddAttributes(trace.Int64Attribute("inputs", int64(len(inputs))))
	results := make([]inputFileResult, len(inputs))
	for i, input := range inputs {
		wg.Add(1)
		go func(input *gomapb.ExecReq_Input, result *inputFileResult) {
			defer wg.Done()
			fname, err := rootRel(input.GetFilename())
			if err != nil {
				if err == errOutOfRoot {
					logger.Warnf("filename %s: %v", input.GetFilename(), err)
					return
				}
				result.err = fmt.Errorf("input file: %s %v", input.GetFilename(), err)
				return
			}

			data, err := gi.toDigest(ctx, input)
			if err != nil {
				result.missingInput = input.GetFilename()
				result.missingReason = fmt.Sprintf("input: %v", err)
				return
			}
			file := merkletree.Entry{
				Name: fname,
				Data: inputDigestData{
					filename: input.GetFilename(),
					Data:     data,
				},
				IsExecutable: executableInputs[input.GetFilename()],
			}
			result.file = file
			if input.Content == nil {
				return
			}
			result.needUpload = true
		}(input, &results[i])
	}
	wg.Wait()
	return results
}

// newInputTree constructs input tree from req.
// it returns non-nil ExecResp for:
// - missing inputs
// - input root detection failed
// - non-relative and non C: drive on windows.
func (r *request) newInputTree(ctx context.Context) *gomapb.ExecResp {
	if r.err != nil {
		return nil
	}
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.request.newInputTree")
	defer span.End()
	logger := log.FromContext(ctx)

	inputPaths, err := inputPaths(r.filepath, r.gomaReq, r.cmdFiles[0].Path)
	if err != nil {
		logger.Errorf("bad input: %v", err)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("bad input: %v", err))
		return r.gomaResp
	}
	execRootDir := r.gomaReq.GetRequesterInfo().GetExecRoot()
	rootDir, needChroot, err := inputRootDir(r.filepath, inputPaths, r.allowChroot, execRootDir)
	if err != nil {
		logger.Errorf("input root detection failed: %v", err)
		logFileList(logger, "input paths", inputPaths)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("input root detection failed: %v", err))
		return r.gomaResp
	}
	r.tree = merkletree.New(r.filepath, rootDir, r.digestStore)
	r.needChroot = needChroot

	logger.Infof("new input tree cwd:%s root:%s execRoot:%s %s", r.gomaReq.GetCwd(), r.tree.RootDir(), execRootDir, r.filepath)
	// If toolchain_included is true, r.gomaReq.Input and cmdFiles will contain the same files.
	// To avoid dup, if it's added in r.gomaReq.Input, we don't add it as cmdFiles.
	// While processing r.gomaReq.Input, we handle missing input, so the main routine is in
	// r.gomaReq.Input.

	// path from cwd -> is_executable. Don't confuse "path from cwd" and "path from input root".
	// Everything (except symlink) in ToolchainSpec should be in r.gomaReq.Input.
	// If not and it's necessary to execute, a runtime error (while compile) can happen.
	// e.g. *.so is missing etc.
	toolchainInputs := make(map[string]bool)
	executableInputs := make(map[string]bool)
	if r.gomaReq.GetToolchainIncluded() {
		for _, ts := range r.gomaReq.ToolchainSpecs {
			if ts.GetSymlinkPath() != "" {
				// If toolchain is a symlink, it is not included in r.gomaReq.Input.
				// So, toolchainInputs should not contain it.
				continue
			}
			toolchainInputs[ts.GetPath()] = true
			if ts.GetIsExecutable() {
				executableInputs[ts.GetPath()] = true
			}
		}
	}

	cleanCWD := r.filepath.Clean(r.gomaReq.GetCwd())
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())

	start := time.Now()
	reqInputs := r.gomaReq.Input
	if _, ok := r.filepath.(winpath.FilePath); ok && !r.cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
		// need to dedup filename for windows,
		// except windows cross case.
		reqInputs = dedupInputs(r.filepath, cleanCWD, r.gomaReq.Input)
		if len(reqInputs) != len(r.gomaReq.Input) {
			logger.Infof("input dedup %d -> %d", len(r.gomaReq.Input), len(reqInputs))
		}
	}
	results := inputFiles(ctx, reqInputs, r.input, func(filename string) (string, error) {
		return rootRel(r.filepath, filename, cleanCWD, cleanRootDir)
	}, executableInputs)
	uploads := make([]*gomapb.ExecReq_Input, 0, len(reqInputs))
	for i, input := range reqInputs {
		result := &results[i]
		if r.err == nil && result.err != nil {
			r.err = result.err
		}
		if result.needUpload {
			uploads = append(uploads, input)
		}
	}
	if r.err != nil {
		logger.Warnf("inputFiles=%d uploads=%d in %s err:%v", len(reqInputs), len(uploads), time.Since(start), r.err)
		return nil
	}
	logger.Infof("inputFiles=%d uploads=%d in %s", len(reqInputs), len(uploads), time.Since(start))

	var files []merkletree.Entry
	var missingInputs []string
	var missingReason []string
	for _, in := range results {
		if in.missingInput != "" {
			missingInputs = append(missingInputs, in.missingInput)
			missingReason = append(missingReason, in.missingReason)
			continue
		}
		if in.file.Name == "" {
			// ignore out of root files.
			continue
		}
		files = append(files, in.file)
	}
	if len(missingInputs) > 0 {
		logger.Infof("missing %d inputs out of %d. need to uploads=%d", len(missingInputs), len(reqInputs), len(uploads))

		r.gomaResp.MissingInput = missingInputs
		r.gomaResp.MissingReason = missingReason
		thinOutMissing(r.gomaResp, missingInputLimit)
		sortMissing(r.gomaReq.Input, r.gomaResp)
		logFileList(logger, "missing inputs", r.gomaResp.MissingInput)
		return r.gomaResp
	}

	// create wrapper scripts
	err = r.newWrapperScript(ctx, r.cmdConfig, r.cmdFiles[0].Path)
	if err != nil {
		var badReqErr badRequestError
		if errors.As(err, &badReqErr) {
			r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
			r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, badReqErr.Error())
			return r.gomaResp
		}
		// otherwise, internal error.
		r.err = fmt.Errorf("wrapper script: %v", err)
		return nil
	}

	symAbsOk := r.f.capabilities.GetCacheCapabilities().GetSymlinkAbsolutePathStrategy() == rpb.SymlinkAbsolutePathStrategy_ALLOWED

	for _, f := range r.cmdFiles {
		if _, found := toolchainInputs[f.Path]; found {
			// Must be processed in r.gomaReq.Input. So, skip this.
			// TODO: cmdFiles should be empty instead if toolchain_included = true case?
			continue
		}

		e, err := fileSpecToEntry(ctx, f, r.f.CmdStorage)
		if err != nil {
			r.err = fmt.Errorf("fileSpecToEntry: %v", err)
			return nil
		}
		if !symAbsOk && e.Target != "" && filepath.IsAbs(e.Target) {
			e, err = changeSymlinkAbsToRel(e)
			if err != nil {
				r.err = err
				return nil
			}
		}
		fname, err := rootRel(r.filepath, e.Name, cleanCWD, cleanRootDir)
		if err != nil {
			if err == errOutOfRoot {
				logger.Warnf("cmd files: out of root: %s", e.Name)
				continue
			}
			r.err = fmt.Errorf("command file: %v", err)
			return nil
		}
		e.Name = fname
		files = append(files, e)
	}

	addDirs := func(name string, dirs []string) {
		if r.err != nil {
			return
		}
		for _, d := range dirs {
			rel, err := rootRel(r.filepath, d, cleanCWD, cleanRootDir)
			if err != nil {
				if err == errOutOfRoot {
					logger.Warnf("%s %s: %v", name, d, err)
					continue
				}
				r.err = fmt.Errorf("%s %s: %v", name, d, err)
				return
			}
			files = append(files, merkletree.Entry{
				// directory
				Name: rel,
			})
		}
	}
	// Set up system include and framework paths (b/119072207)
	// -isystem etc can be set for a compile, and non-existence of a directory specified by -isystem may cause compile error even if no file inside the directory is used.
	addDirs("cxx system include path", r.gomaReq.GetCommandSpec().GetCxxSystemIncludePath())
	addDirs("system include path", r.gomaReq.GetCommandSpec().GetSystemIncludePath())
	addDirs("system framework path", r.gomaReq.GetCommandSpec().GetSystemFrameworkPath())

	// prepare output dirs.
	r.outputs = outputs(ctx, r.cmdConfig, r.gomaReq)
	var outDirs []string
	for _, d := range r.outputs {
		outDirs = append(outDirs, r.filepath.Dir(d))
	}
	addDirs("output file", outDirs)
	r.outputDirs = outputDirs(ctx, r.cmdConfig, r.gomaReq)
	addDirs("output dir", r.outputDirs)
	if r.err != nil {
		return nil
	}

	for _, f := range files {
		err = r.tree.Set(f)
		if err != nil {
			r.err = fmt.Errorf("input file: %v: %v", f, err)
			return nil
		}
	}

	root, err := r.tree.Build(ctx)
	if err != nil {
		r.err = err
		return nil
	}
	logger.Infof("input root digest: %v", root)
	r.action.InputRootDigest = root

	// uploads embedded contents to file-server
	// for the case the file was not yet uploaded to RBE CAS.
	// even if client sends input with embedded content,
	// the content may be already uploaded to RBE CAS,
	// and uploaded content may not be needed,
	// so we could ignore error of these uploads.
	start = time.Now()
	err = uploadInputFiles(ctx, uploads, r.input)
	logger.Infof("upload %d inputs out of %d in %s: %v", len(uploads), len(r.gomaReq.Input), time.Since(start), err)
	return nil
}

type wrapperType int

const (
	wrapperRelocatable wrapperType = iota
	wrapperInputRootAbsolutePath
	wrapperNsjailChroot
	wrapperWin
	wrapperWinInputRootAbsolutePath
)

func (w wrapperType) String() string {
	switch w {
	case wrapperRelocatable:
		return "wrapper-relocatable"
	case wrapperInputRootAbsolutePath:
		return "wrapper-input-root-absolute-path"
	case wrapperNsjailChroot:
		return "wrapper-nsjail-chroot"
	case wrapperWin:
		return "wrapper-win"
	case wrapperWinInputRootAbsolutePath:
		return "wrapper-win-input-root-absolute-path"
	default:
		return fmt.Sprintf("wrapper-unknown-%d", int(w))
	}
}

const (
	// TODO: use working_directory in action.
	// need to fix output path to be relative to working_directory.
	// http://b/113370588
	wrapperScript = `#!/bin/bash
set -e
if [[ "$WORK_DIR" != "" ]]; then
  cd "${WORK_DIR}"
fi
exec "$@"
`
)

type badRequestError struct {
	err error
}

func (b badRequestError) Error() string {
	return b.err.Error()
}

// TODO: put wrapper script in platform container?
func (r *request) newWrapperScript(ctx context.Context, cmdConfig *cmdpb.Config, argv0 string) error {
	logger := log.FromContext(ctx)

	cwd := r.gomaReq.GetCwd()
	cleanCWD := r.filepath.Clean(cwd)
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())
	wd, err := rootRel(r.filepath, cwd, cleanCWD, cleanRootDir)
	if err != nil {
		return badRequestError{err: fmt.Errorf("bad cwd=%s: %v", cwd, err)}
	}
	if wd == "" {
		wd = "."
	}
	if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
		wd = winpath.ToPosix(wd)
	}
	envs := []string{fmt.Sprintf("WORK_DIR=%s", wd)}

	// The developer of this program can make multiple wrapper scripts
	// to be used by adding fileDesc instances to `files`.
	// However, only the first one is called in the command line.
	// The other scripts should be called from the first wrapper script
	// if needed.
	var files []merkletree.Entry

	args := buildArgs(ctx, cmdConfig, argv0, r.gomaReq)
	// TODO: only allow specific envs.
	r.crossTarget = targetFromArgs(args)

	var relocatableErr error
	wt := wrapperRelocatable
	switch r.filepath.(type) {
	case posixpath.FilePath:
		if r.needChroot {
			wt = wrapperNsjailChroot
		} else {
			relocatableErr = relocatableReq(ctx, cmdConfig, r.filepath, r.gomaReq.Arg, r.gomaReq.Env)
			if relocatableErr != nil {
				wt = wrapperInputRootAbsolutePath
				logger.Infof("non relocatable: %v", relocatableErr)
			}
		}
	case winpath.FilePath:
		relocatableErr = relocatableReq(ctx, cmdConfig, r.filepath, r.gomaReq.Arg, r.gomaReq.Env)
		if relocatableErr != nil {
			wt = wrapperWinInputRootAbsolutePath
			logger.Infof("non relocatable: %v", relocatableErr)
		} else {
			wt = wrapperWin
		}
		if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
			switch wt {
			case wrapperWinInputRootAbsolutePath:
				// we expect most case is relocatable
				// with -fdebug-compilation-dir=.
				// but it would break if user uses unknown
				// flags, which makes request unrelocatable.
				// See rootDir fix in wrapperInputRootAbsolutePath below.
				wt = wrapperInputRootAbsolutePath
			case wrapperWin:
				wt = wrapperRelocatable
			}
		}
	default:
		// internal error? maybe toolchain config is broken.
		return fmt.Errorf("bad path type: %T", r.filepath)
	}

	const posixWrapperName = "run.sh"
	switch wt {
	case wrapperNsjailChroot:
		logger.Infof("run with nsjail chroot")
		// needed for bind mount.
		r.addPlatformProperty(ctx, "dockerPrivileged", "true")
		// needed for chroot command and mount command.
		r.addPlatformProperty(ctx, "dockerRunAsRoot", "true")
		nsjailCfg := nsjailChrootConfig(cwd, r.filepath, r.gomaReq.GetToolchainSpecs(), r.gomaReq.Env)
		files = []merkletree.Entry{
			{
				Name:         posixWrapperName,
				Data:         digest.Bytes("nsjail-chroot-run-wrapper-script", []byte(nsjailChrootRunWrapperScript)),
				IsExecutable: true,
			},
			{
				Name: "nsjail.cfg",
				Data: digest.Bytes("nsjail-config-file", []byte(nsjailCfg)),
			},
		}
	case wrapperInputRootAbsolutePath:
		wrapperData := digest.Bytes("wrapper-script", []byte(wrapperScript))
		files, wrapperData = r.maybeApplyHardening(ctx, "InputRootAbsolutePath", files, wrapperData)
		// https://cloud.google.com/remote-build-execution/docs/remote-execution-properties#container_properties
		rootDir := r.tree.RootDir()
		if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
			// we can't use windows path as absolute path.
			// drop first two letters (i.e. `C:`), and
			// convert \ to /.
			// instead of making relocatable check loose,
			// better to omit drive letter and make the
			// effective for the same drive letter.
			rootDir = winpath.ToPosix(rootDir)
		}
		r.addPlatformProperty(ctx, "InputRootAbsolutePath", rootDir)
		for _, e := range r.gomaReq.Env {
			envs = append(envs, e)
		}
		files = append([]merkletree.Entry{
			{
				Name:         posixWrapperName,
				Data:         wrapperData,
				IsExecutable: true,
			},
		}, files...)
	case wrapperRelocatable:
		wrapperData := digest.Bytes("wrapper-script", []byte(wrapperScript))
		files, wrapperData = r.maybeApplyHardening(ctx, "chdir: relocatble", files, wrapperData)
		for _, e := range r.gomaReq.Env {
			if strings.HasPrefix(e, "PWD=") {
				// PWD is usually absolute path.
				// if relocatable, then we should remove
				// PWD environment variable.
				continue
			}
			envs = append(envs, e)
		}
		files = append([]merkletree.Entry{
			{
				Name:         posixWrapperName,
				Data:         wrapperData,
				IsExecutable: true,
			},
		}, files...)
	case wrapperWin:
		logger.Infof("run on win")
		wn, data, err := wrapperForWindows(ctx)
		if err != nil {
			// missing run.exe?
			return err
		}
		// no need to set environment variables??
		files = []merkletree.Entry{
			{
				Name:         wn,
				Data:         data,
				IsExecutable: true,
			},
		}
	case wrapperWinInputRootAbsolutePath:
		logger.Infof("run on win with InputRootAbsolutePath")
		if relocatableErr != nil && !strings.HasPrefix(strings.ToUpper(r.tree.RootDir()), `C:\`) {
			// TODO Docker Internal Errors
			// see also http://b/161274896 Catch specific case where drive letter other than C: specified for input root on Windows
			logger.Errorf("non relocatable on windows, but absolute path is not C: drive. %s", r.tree.RootDir())
			return badRequestError{err: fmt.Errorf("non relocatable %v, but root dir is %q. make request relocatable, or use `C:`", relocatableErr, r.tree.RootDir())}
		}
		// https://cloud.google.com/remote-build-execution/docs/remote-execution-properties#container_properties
		r.addPlatformProperty(ctx, "InputRootAbsolutePath", r.tree.RootDir())
		wn, data, err := wrapperForWindows(ctx)
		if err != nil {
			// missing run.exe?
			return err
		}
		// This is necessary for Win emscripten-releases LLVM build, which uses env vars to specify e.g. include
		// dirs. See crbug.com/1040150.
		// The build uses abs paths, which is why the env vars are stored here. Whether or not they should also
		// be in stored in the case of `wrapperWin` is left for future consideration.
		for _, e := range r.gomaReq.Env {
			if strings.HasPrefix(e, "INCLUDE=") || strings.HasPrefix(e, "LIB=") {
				envs = append(envs, e)
			}
		}
		files = []merkletree.Entry{
			{
				Name:         wn,
				Data:         data,
				IsExecutable: true,
			},
		}
	default:
		// coding error?
		return fmt.Errorf("bad wrapper type: %v", wt)
	}

	// Only the first one is called in the command line via storing
	// `wrapperPath` in `r.args` later.
	wrapperPath := ""
	for i, w := range files {
		w.Name, err = rootRel(r.filepath, w.Name, cleanCWD, cleanRootDir)
		if err != nil {
			// rootRel should not fail with any user input at this point?
			return err
		}

		logger.Infof("file (%d) %s => %v", i, w.Name, w.Data.Digest())
		r.tree.Set(w)
		if wrapperPath == "" {
			wrapperPath = w.Name
		}
	}

	r.envs = envs

	// if a wrapper exists in cwd, `wrapper` does not have a directory name.
	// It cannot be callable on POSIX because POSIX do not contain "." in
	// its PATH.
	if wrapperPath == posixWrapperName {
		wrapperPath = "./" + posixWrapperName
	}
	if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
		wrapperPath = winpath.ToPosix(wrapperPath)
	}
	r.args = append([]string{wrapperPath}, args...)

	err = stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(wrapperTypeKey, wt.String())}, wrapperCount.M(1))
	if err != nil {
		logger.Errorf("record wrapper-count %s: %v", wt, err)
	}
	return nil
}

func (r *request) maybeApplyHardening(ctx context.Context, wt string, files []merkletree.Entry, wrapperData digest.Data) ([]merkletree.Entry, digest.Data) {
	logger := log.FromContext(ctx)
	if f, disable := disableHardening(r.f.DisableHardenings, r.cmdFiles); disable {
		logger.Infof("run with %s (disable hardening for %v)", wt, f)
	} else if rand.Float64() < r.f.HardeningRatio {
		if rand.Float64() < r.f.NsjailRatio {
			logger.Infof("run with %s + nsjail", wt)
			wrapperData = digest.Bytes("nsjail-hardening-wrapper-scrpt", []byte(nsjailHardeningWrapperScript))
			// needed for nsjail
			r.addPlatformProperty(ctx, "dockerPrivileged", "true")
			files = append(files, merkletree.Entry{
				Name: "nsjail.cfg",
				Data: digest.Bytes("nsjail.cfg", []byte(nsjailHardeningConfig)),
			})
		} else {
			logger.Infof("run with %s + runsc", wt)
			r.addPlatformProperty(ctx, "dockerRuntime", "runsc")
		}
	} else {
		logger.Infof("run with %s", wt)
	}
	return files, wrapperData
}

func disableHardening(hashes []string, cmdFiles []*cmdpb.FileSpec) (*cmdpb.FileSpec, bool) {
	for _, h := range hashes {
		if h == "" {
			continue
		}
		for _, f := range cmdFiles {
			if f.GetSymlink() != "" {
				continue
			}
			if f.GetHash() == h {
				return f, true
			}
		}
	}
	return nil, false
}

// TODO: refactor with exec/clang.go, exec/clangcl.go?

// buildArgs builds args in RBE from arg0 and req, respecting cmdConfig.
func buildArgs(ctx context.Context, cmdConfig *cmdpb.Config, arg0 string, req *gomapb.ExecReq) []string {
	// TODO: need compiler specific handling?
	args := append([]string{arg0}, req.Arg[1:]...)
	if cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
		args[0] = winpath.ToPosix(args[0])
		pathFlag := false
	argLoop:
		for i := 1; i < len(args); i++ {
			if pathFlag {
				args[i] = winpath.ToPosix(args[i])
				pathFlag = false
				continue argLoop
			}
			// JoinedOrSeparate
			for _, f := range []string{"/winsysroot", "-winsysroot", "-imsvc", "/imsvc", "-I", "/I"} {
				if args[i] == f {
					pathFlag = true
					continue argLoop
				}
				if strings.HasPrefix(args[i], f) {
					args[i] = f + winpath.ToPosix(strings.TrimPrefix(args[i], f))
					continue argLoop
				}
			}
			// Joined
			// Fd is ignored, though
			for _, f := range []string{"-resource-dir=", "/Fo", "-Fo", "/Fd", "-Fd"} {
				if strings.HasPrefix(args[i], f) {
					args[i] = f + winpath.ToPosix(strings.TrimPrefix(args[i], f))
					continue argLoop
				}
			}
			// TODO: need to handle other args?
			if strings.HasPrefix(args[i], "-") || strings.HasPrefix(args[i], "/") {
				continue argLoop
			}
			// input file, or arg of some flag?
			// assume arg of some flag (e.g. -D) won't be windows
			// absolute path.
			if winpath.IsAbs(args[i]) {
				args[i] = winpath.ToPosix(args[i])
				continue argLoop
			}
		}
		envs := req.Env
		req.Env = nil
		for _, e := range envs {
			switch {
			case strings.HasPrefix(e, "INCLUDE="):
				includes := strings.Split(strings.TrimPrefix(e, "INCLUDE="), ";")
				for _, inc := range includes {
					args = append(args, "-imsvc"+winpath.ToPosix(inc))
				}
			case strings.HasPrefix(e, "LIB="):
				// unnecessary?
			default:
				req.Env = append(req.Env, e)
			}
		}

	}
	if cmdConfig.GetCmdDescriptor().GetCross().GetClangNeedTarget() {
		args = addTargetIfNotExist(args, req.GetCommandSpec().GetTarget())
	}
	return args
}

// add target option to args if args doesn't already have target option.
func addTargetIfNotExist(args []string, target string) []string {
	// no need to add -target if arg already have it.
	for _, arg := range args {
		if arg == "-target" || strings.HasPrefix(arg, "--target=") {
			return args
		}
	}
	// https://clang.llvm.org/docs/CrossCompilation.html says
	// `-target <triple>`, but clang --help shows
	//  --target=<value>        Generate code for the given target
	return append(args, fmt.Sprintf("--target=%s", target))
}

func targetFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-target" {
			if i < len(args)-1 {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "--target=") {
			return strings.TrimPrefix(arg, "--target=")
		}
	}
	return ""
}

type unknownFlagError struct {
	arg string
}

func (e unknownFlagError) Error() string {
	return fmt.Sprintf("unknown flag: %s", e.arg)
}

// relocatableReq checks args, envs is relocatable, respecting cmdConfig.
func relocatableReq(ctx context.Context, cmdConfig *cmdpb.Config, filepath clientFilePath, args, envs []string) error {
	name := cmdConfig.GetCmdDescriptor().GetSelector().GetName()
	var err error
	switch name {
	case "gcc", "g++", "clang", "clang++":
		err = gccRelocatableReq(filepath, args, envs)
	case "clang-cl":
		err = clangclRelocatableReq(filepath, args, envs)
	case "javac":
		// Currently, javac in Chromium is fully relocatable. Simpler just to
		// support only the relocatable case and let it fail if the client passed
		// in invalid absolute paths.
		err = nil
	default:
		// "cl.exe", "clang-tidy"
		err = fmt.Errorf("no relocatable check for %s", name)
	}
	if err != nil {
		var uerr unknownFlagError
		if errors.As(err, &uerr) {
			if serr := stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(compilerNameKey, name)}, unknownFlagCount.M(1)); serr != nil {
				logger := log.FromContext(ctx)
				logger.Errorf("record unknown-flag %s: %v", name, serr)
			}
		}
	}
	return err
}

// outputs gets output filenames from gomaReq.
// If either expected_output_files or expected_output_dirs is specified,
// expected_output_files is used.
// Otherwise, it's calculated from args.
func outputs(ctx context.Context, cmdConfig *cmdpb.Config, gomaReq *gomapb.ExecReq) []string {
	if len(gomaReq.ExpectedOutputFiles) > 0 || len(gomaReq.ExpectedOutputDirs) > 0 {
		return gomaReq.GetExpectedOutputFiles()
	}

	args := gomaReq.Arg
	switch name := cmdConfig.GetCmdDescriptor().GetSelector().GetName(); name {
	case "gcc", "g++", "clang", "clang++":
		return gccOutputs(args)
	case "clang-cl":
		return clangclOutputs(args)
	default:
		// "cl.exe", "javac", "clang-tidy"
		return nil
	}
}

// outputDirs gets output dirnames from gomaReq.
// If either expected_output_files or expected_output_dirs is specified,
// expected_output_dirs is used.
// Otherwise, it's calculated from args.
func outputDirs(ctx context.Context, cmdConfig *cmdpb.Config, gomaReq *gomapb.ExecReq) []string {
	if len(gomaReq.ExpectedOutputFiles) > 0 || len(gomaReq.ExpectedOutputDirs) > 0 {
		return gomaReq.GetExpectedOutputDirs()
	}

	args := gomaReq.Arg
	switch cmdConfig.GetCmdDescriptor().GetSelector().GetName() {
	case "javac":
		return javacOutputDirs(args)
	default:
		return nil
	}
}

func (r *request) setupNewAction(ctx context.Context) {
	if r.err != nil {
		return
	}
	command, err := r.newCommand(ctx)
	if err != nil {
		r.err = err
		return
	}

	// we'll run  wrapper script that chdir, so don't set chdir here.
	// see newWrapperScript.
	// TODO: set command.WorkingDirectory
	data, err := digest.Proto(command)
	if err != nil {
		r.err = err
		return
	}
	logger := log.FromContext(ctx)
	logger.Infof("command digest: %v", data.Digest())

	r.digestStore.Set(data)
	r.action.CommandDigest = data.Digest()

	data, err = digest.Proto(r.action)
	if err != nil {
		r.err = err
		return
	}
	r.digestStore.Set(data)
	logger.Infof("action digest: %v %s", data.Digest(), r.action)
	r.actionDigest = data.Digest()
}

func (r *request) newCommand(ctx context.Context) (*rpb.Command, error) {
	logger := log.FromContext(ctx)

	envVars := createEnvVars(ctx, r.envs)
	sort.Slice(r.platform.Properties, func(i, j int) bool {
		return r.platform.Properties[i].Name < r.platform.Properties[j].Name
	})
	command := &rpb.Command{
		Arguments:            r.args,
		EnvironmentVariables: envVars,
		Platform:             r.platform,
	}

	logger.Debugf("setup for outputs: %v", r.outputs)
	cleanCWD := r.filepath.Clean(r.gomaReq.GetCwd())
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())
	// set output files from command line flags.
	for _, output := range r.outputs {
		rel, err := rootRel(r.filepath, output, cleanCWD, cleanRootDir)
		if err != nil {
			return nil, fmt.Errorf("output %s: %v", output, err)
		}
		if r.cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
			rel = winpath.ToPosix(rel)
		}
		command.OutputFiles = append(command.OutputFiles, rel)
	}
	sort.Strings(command.OutputFiles)

	logger.Debugf("setup for output dirs: %v", r.outputDirs)
	// set output dirs from command line flags.
	for _, output := range r.outputDirs {
		rel, err := rootRel(r.filepath, output, cleanCWD, cleanRootDir)
		if err != nil {
			return nil, fmt.Errorf("output dir %s: %v", output, err)
		}
		if r.cmdConfig.GetCmdDescriptor().GetCross().GetWindowsCross() {
			rel = winpath.ToPosix(rel)
		}
		command.OutputDirectories = append(command.OutputDirectories, rel)
	}
	sort.Strings(command.OutputDirectories)

	return command, nil
}

func (r *request) checkCache(ctx context.Context) (*rpb.ActionResult, bool) {
	if r.err != nil {
		// no need to ask to execute.
		return nil, true
	}
	logger := log.FromContext(ctx)
	if skipCacheLookup(r.gomaReq) {
		logger.Infof("store_only; skip cache lookup")
		return nil, false
	}
	resp, err := r.client.Cache().GetActionResult(ctx, &rpb.GetActionResultRequest{
		InstanceName: r.instanceName(),
		ActionDigest: r.actionDigest,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			logger.Infof("no cached action %v: %v", r.actionDigest, err)
		case codes.Unavailable, codes.Canceled, codes.Aborted:
			logger.Warnf("get action result %v: %v", r.actionDigest, err)
		default:
			logger.Errorf("get action result %v: %v", r.actionDigest, err)
		}
		return nil, false
	}
	return resp, true
}

func (r *request) missingBlobs(ctx context.Context) ([]*rpb.Digest, error) {
	if r.err != nil {
		return nil, r.err
	}
	var blobs []*rpb.Digest
	err := rpc.Retry{}.Do(ctx, func() error {
		var err error
		blobs, err = r.cas.Missing(ctx, r.instanceName(), r.digestStore.List())
		return fixRBEInternalError(err)
	})
	if err != nil {
		r.err = err
		return nil, err
	}
	return blobs, nil
}

func inputForDigest(ds *digest.Store, d *rpb.Digest) (string, error) {
	src, ok := ds.GetSource(d)
	if !ok {
		return "", fmt.Errorf("not found for %s", d)
	}
	idd, ok := src.(inputDigestData)
	if !ok {
		return "", fmt.Errorf("not input file for %s", d)
	}
	return idd.filename, nil
}

type byInputFilenames struct {
	order map[string]int
	resp  *gomapb.ExecResp
}

func (b byInputFilenames) Len() int { return len(b.resp.MissingInput) }
func (b byInputFilenames) Swap(i, j int) {
	b.resp.MissingInput[i], b.resp.MissingInput[j] = b.resp.MissingInput[j], b.resp.MissingInput[i]
	b.resp.MissingReason[i], b.resp.MissingReason[j] = b.resp.MissingReason[j], b.resp.MissingReason[i]
}

func (b byInputFilenames) Less(i, j int) bool {
	io := b.order[b.resp.MissingInput[i]]
	jo := b.order[b.resp.MissingInput[j]]
	return io < jo
}

func sortMissing(inputs []*gomapb.ExecReq_Input, resp *gomapb.ExecResp) {
	m := make(map[string]int)
	for i, input := range inputs {
		m[input.GetFilename()] = i
	}
	sort.Sort(byInputFilenames{
		order: m,
		resp:  resp,
	})
}

// The server does not report more than this size as missing inputs to avoid DoS from Goma client.
const missingInputLimit = 100

// thinOutMissing thins out missint inputs if it is more than limit.
// Note: sortMissing should be called after this to preserve the file name order.
func thinOutMissing(resp *gomapb.ExecResp, limit int) {
	if len(resp.MissingInput) < limit { // no need to thin out.
		return
	}
	rand.Shuffle(len(resp.MissingInput), func(i, j int) {
		resp.MissingInput[i], resp.MissingInput[j] = resp.MissingInput[j], resp.MissingInput[i]
	})
	resp.MissingInput = resp.MissingInput[:limit]
}

func logFileList(logger log.Logger, msg string, files []string) {
	s := fmt.Sprintf("%q", files)
	const logLineThreshold = 95 * 1024
	if len(s) < logLineThreshold {
		logger.Infof("%s %s", msg, s)
		return
	}
	logger.Warnf("too many %s %d", msg, len(files))
	var b strings.Builder
	var i int
	for len(files) > 0 {
		if b.Len() > 0 {
			fmt.Fprintf(&b, " ")
		}
		s, files = files[0], files[1:]
		fmt.Fprintf(&b, "%q", s)
		if b.Len() > logLineThreshold {
			logger.Infof("%s %d: [%s]", msg, i, b.String())
			i++
			b.Reset()
		}
	}
	if b.Len() > 0 {
		logger.Infof("%s %d: [%s]", msg, i, b)
	}
}

func (r *request) uploadBlobs(ctx context.Context, blobs []*rpb.Digest) (*gomapb.ExecResp, error) {
	if r.err != nil {
		return nil, r.err
	}
	err := r.cas.Upload(ctx, r.instanceName(), r.f.CASBlobLookupSema, blobs...)
	if err != nil {
		if missing, ok := err.(cas.MissingError); ok {
			logger := log.FromContext(ctx)
			logger.Infof("failed to upload blobs %s", missing.Blobs)
			var missingInputs []string
			var missingReason []string
			for _, b := range missing.Blobs {
				fname, err := inputForDigest(r.digestStore, b.Digest)
				if err != nil {
					logger.Warnf("unknown input for %s: %v", b.Digest, err)
					continue
				}
				missingInputs = append(missingInputs, fname)
				missingReason = append(missingReason, b.Err.Error())
			}
			if len(missingInputs) > 0 {
				r.gomaResp.MissingInput = missingInputs
				r.gomaResp.MissingReason = missingReason
				thinOutMissing(r.gomaResp, missingInputLimit)
				sortMissing(r.gomaReq.Input, r.gomaResp)
				logFileList(logger, "missing inputs", r.gomaResp.MissingInput)
				return r.gomaResp, nil
			}
			// failed to upload non-input, so no need to report
			// missing input to users.
			// handle it as grpc error.
		}
		r.err = err
	}
	return nil, err
}

func (r *request) executeAction(ctx context.Context) (*rpb.ExecuteResponse, error) {
	if r.err != nil {
		return nil, r.Err()
	}
	_, resp, err := ExecuteAndWait(ctx, r.client, &rpb.ExecuteRequest{
		InstanceName:    r.instanceName(),
		SkipCacheLookup: skipCacheLookup(r.gomaReq),
		ActionDigest:    r.actionDigest,
		// ExecutionPolicy
		// ResultsCachePolicy
	})
	if err != nil {
		r.err = err
		return nil, r.Err()
	}
	return resp, nil
}

func timestampSub(ctx context.Context, t1, t2 *tspb.Timestamp) time.Duration {
	time1 := t1.AsTime()
	time2 := t2.AsTime()
	return time1.Sub(time2)
}

func (r *request) newResp(ctx context.Context, eresp *rpb.ExecuteResponse, cached bool) (*gomapb.ExecResp, error) {
	logger := log.FromContext(ctx)
	if r.err != nil {
		return nil, r.Err()
	}
	logger.Debugf("response %v cached=%t", eresp, cached)
	r.gomaResp.CacheKey = proto.String(r.actionDigest.String())
	switch {
	case eresp.CachedResult:
		r.gomaResp.CacheHit = gomapb.ExecResp_STORAGE_CACHE.Enum()
	case cached:
		r.gomaResp.CacheHit = gomapb.ExecResp_MEM_CACHE.Enum()
	default:
		r.gomaResp.CacheHit = gomapb.ExecResp_NO_CACHE.Enum()
	}
	if st := eresp.GetStatus(); st.GetCode() != 0 {
		logger.Errorf("execute status error: %v", st)
		s := status.FromProto(st)
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("Execute error: %s", s.Code()))
		logger.Errorf("resp %v", r.gomaResp)
		return r.gomaResp, nil
	}
	if eresp.Result == nil {
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, "unexpected response message")
		logger.Errorf("resp %v", r.gomaResp)
		return r.gomaResp, nil
	}
	md := eresp.Result.GetExecutionMetadata()
	queueTime := timestampSub(ctx, md.GetWorkerStartTimestamp(), md.GetQueuedTimestamp())
	workerTime := timestampSub(ctx, md.GetWorkerCompletedTimestamp(), md.GetWorkerStartTimestamp())
	inputTime := timestampSub(ctx, md.GetInputFetchCompletedTimestamp(), md.GetInputFetchStartTimestamp())
	execTime := timestampSub(ctx, md.GetExecutionCompletedTimestamp(), md.GetExecutionStartTimestamp())
	outputTime := timestampSub(ctx, md.GetOutputUploadCompletedTimestamp(), md.GetOutputUploadStartTimestamp())
	osFamily := platformOSFamily(r.platform)
	dockerRuntime := platformDockerRuntime(r.platform)
	crossCompileType := crossCompileType(r.cmdConfig.GetCmdDescriptor().GetCross())
	logger.Infof("exit=%d cache=%s : exec on %q[%s, %s, cross:%s, target=%s] queue=%s worker=%s input=%s exec=%s output=%s",
		eresp.Result.GetExitCode(),
		r.gomaResp.GetCacheHit(),
		md.GetWorker(),
		osFamily,
		dockerRuntime,
		crossCompileType,
		r.crossTarget,
		queueTime,
		workerTime,
		inputTime,
		execTime,
		outputTime)
	tags := []tag.Mutator{
		// exit_code=159 is seccomp violation.
		tag.Upsert(rbeExitKey, fmt.Sprintf("%d", eresp.Result.GetExitCode())),
		tag.Upsert(rbeCacheKey, r.gomaResp.GetCacheHit().String()),
		tag.Upsert(rbePlatformOSFamilyKey, osFamily),
		tag.Upsert(rbePlatformDockerRuntimeKey, dockerRuntime),
		tag.Upsert(rbeCrossKey, crossCompileType),
	}
	stats.RecordWithTags(ctx, tags, rbeQueueTime.M(float64(queueTime.Nanoseconds())/1e6))
	stats.RecordWithTags(ctx, tags, rbeWorkerTime.M(float64(workerTime.Nanoseconds())/1e6))
	stats.RecordWithTags(ctx, tags, rbeInputTime.M(float64(inputTime.Nanoseconds())/1e6))
	stats.RecordWithTags(ctx, tags, rbeExecTime.M(float64(execTime.Nanoseconds())/1e6))
	stats.RecordWithTags(ctx, tags, rbeOutputTime.M(float64(outputTime.Nanoseconds())/1e6))

	r.gomaResp.ExecutionStats = &gomapb.ExecutionStats{
		ExecutionStartTimestamp:     md.GetExecutionStartTimestamp(),
		ExecutionCompletedTimestamp: md.GetExecutionCompletedTimestamp(),
	}
	gout := gomaOutput{
		gomaResp: r.gomaResp,
		bs:       r.client.ByteStream(),
		instance: r.instanceName(),
		gomaFile: r.f.GomaFile,
	}
	// gomaOutput should return err for codes.Unauthenticated,
	// instead of setting ErrorMessage in r.gomaResp,
	// so it returns to caller (i.e. frontend), and retry with new
	// refreshed oauth2 access token.
	for _, f := range []func(context.Context, *rpb.ExecuteResponse) error{
		gout.stdoutData,
		gout.stderrData,
	} {
		err := f(ctx, eresp)
		if status.Code(err) == codes.Unauthenticated && r.err == nil {
			r.err = err
			return r.gomaResp, r.Err()
		}
	}

	if len(r.gomaResp.Result.StdoutBuffer) > 0 {
		// docker failure would be error of goma server, not users.
		// so make it internal error, rather than command execution error.
		// http://b/80272874
		const dockerErrorResponse = "docker: Error response from daemon: oci runtime error:"
		if eresp.Result.ExitCode == 127 &&
			bytes.Contains(r.gomaResp.Result.StdoutBuffer, []byte(dockerErrorResponse)) {
			logger.Errorf("docker error response %s", shortLogMsg(r.gomaResp.Result.StdoutBuffer))
			return r.gomaResp, status.Errorf(codes.Internal, "docker error: %s", string(r.gomaResp.Result.StdoutBuffer))
		}

		if eresp.Result.ExitCode != 0 {
			logLLVMError(logger, "stdout", r.gomaResp.Result.StdoutBuffer)
		}
		logger.Infof("stdout %s", shortLogMsg(r.gomaResp.Result.StdoutBuffer))
	}
	if len(r.gomaResp.Result.StderrBuffer) > 0 {
		if eresp.Result.ExitCode != 0 {
			logLLVMError(logger, "stderr", r.gomaResp.Result.StderrBuffer)
		}
		logger.Infof("stderr %s", shortLogMsg(r.gomaResp.Result.StderrBuffer))
	}

	for _, output := range eresp.Result.OutputFiles {
		if r.err != nil {
			break
		}
		// output.Path should not be absolute, but relative to root dir.
		// convert it to fname, which is cwd relative.
		fname, err := r.filepath.Rel(r.gomaReq.GetCwd(), r.filepath.Join(r.tree.RootDir(), output.Path))
		if err != nil {
			r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("output path %s: %v", output.Path, err))
			continue
		}
		err = gout.outputFile(ctx, fname, output)
		if err != nil && r.err == nil {
			r.err = err
			return r.gomaResp, r.Err()
		}
	}
	for _, output := range eresp.Result.OutputDirectories {
		if r.err != nil {
			break
		}
		// output.Path should not be absolute, but relative to root dir.
		// convert it to fname, which is cwd relative.
		fname, err := r.filepath.Rel(r.gomaReq.GetCwd(), r.filepath.Join(r.tree.RootDir(), output.Path))
		if err != nil {
			r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("output path %s: %v", output.Path, err))
			continue
		}
		err = gout.outputDirectory(ctx, r.filepath, fname, output, r.f.OutputFileSema)
		if err != nil && r.err == nil {
			r.err = err
			return r.gomaResp, r.Err()
		}
	}
	if len(r.gomaResp.ErrorMessage) == 0 {
		r.gomaResp.Result.ExitStatus = proto.Int32(eresp.Result.ExitCode)
	}

	sizeLimit := exec.DefaultMaxRespMsgSize
	respSize := proto.Size(r.gomaResp)
	if respSize > sizeLimit {
		logger.Infof("gomaResp size=%d, limit=%d, using FileService for larger blobs.", respSize, sizeLimit)
		if err := gout.reduceRespSize(ctx, sizeLimit, r.f.OutputFileSema); err != nil {
			// Don't need to append any error messages to `r.gomaResp` because it won't be sent.
			return nil, fmt.Errorf("failed to reduce resp size below limit=%d, %d -> %d: %v", sizeLimit, respSize, proto.Size(gout.gomaResp), err)
		}
		logger.Infof("gomaResp size reduced %d -> %d", respSize, proto.Size(gout.gomaResp))
	}

	return r.gomaResp, r.Err()
}

func platformOSFamily(p *rpb.Platform) string {
	for _, p := range p.Properties {
		if p.Name == "OSFamily" {
			return p.Value
		}
	}
	return "unspecified"
}

func platformDockerRuntime(p *rpb.Platform) string {
	priv := false
	runAsRoot := false
	for _, p := range p.Properties {
		switch p.Name {
		case "dockerRuntime":
			return p.Value
		case "dockerPrivileged":
			priv = p.Value == "true"
		case "dockerRunAsRoot":
			runAsRoot = p.Value == "true"
		}
	}
	switch {
	case priv && runAsRoot:
		return "nsjail-chroot"
	case priv:
		return "nsjail"
	}
	return "default"
}

func crossCompileType(cross *cmdpb.CmdDescriptor_Cross) string {
	switch {
	case cross.GetWindowsCross():
		return "win"
	case cross.GetClangNeedTarget():
		return "need-target"
	}
	return "no"
}

func shortLogMsg(msg []byte) string {
	if len(msg) <= 1024 {
		return string(msg)
	}
	var b strings.Builder
	b.Write(msg[:512])
	fmt.Fprint(&b, "...")
	b.Write(msg[len(msg)-512:])
	return b.String()
}

// logLLVMError records LLVM ERROR.
// http://b/145177862
func logLLVMError(logger log.Logger, id string, msg []byte) {
	llvmErrorMsg, ok := extractLLVMError(msg)
	if !ok {
		return
	}
	logger.Errorf("%s: %s", id, llvmErrorMsg)
}

func extractLLVMError(msg []byte) ([]byte, bool) {
	const llvmError = "LLVM ERROR:"
	i := bytes.Index(msg, []byte(llvmError))
	if i < 0 {
		return nil, false
	}
	llvmErrorMsg := msg[i:]
	i = bytes.IndexAny(llvmErrorMsg, "\r\n")
	if i >= 0 {
		llvmErrorMsg = llvmErrorMsg[:i]
	}
	return llvmErrorMsg, true
}
