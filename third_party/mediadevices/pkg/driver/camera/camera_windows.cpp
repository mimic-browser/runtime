#include <iostream>
#include <vector>
#include <unistd.h>

#include <dshow.h>
#include <dvdmedia.h>
#include <qedit.h>
#include <mmsystem.h>

#include "camera_windows.hpp"
#include "_cgo_export.h"

static const uint32_t FOURCC_NV12 = 0x3231564E; // 'NV12'
static const uint32_t FOURCC_YUY2 = 0x32595559; // 'YUY2'

// freeMediaType frees an AM_MEDIA_TYPE* allocated by GetStreamCaps.
static void freeMediaType(AM_MEDIA_TYPE* mt)
{
  if (mt == nullptr) return;
  if (mt->cbFormat != 0)
    CoTaskMemFree(mt->pbFormat);
  if (mt->pUnk != nullptr)
    mt->pUnk->Release();
  CoTaskMemFree(mt);
}


imageProp* getProp(camera* cam, int i)
{
  return &cam->props[i];
}

char* getName(cameraList* list, int i)
{
  return list->name[i];
}

char* getFriendlyName(cameraList* list, int i)
{
  return list->friendlyName[i];
}


// printErr shows string representation of HRESULT.
// This is for debugging.
void printErr(HRESULT hr)
{
  char buf[128];
  AMGetErrorTextA(hr, buf, 128);
  fprintf(stderr, "%s\n", buf);
}

// getCameraName returns the display name (device path) of the device.
// returned pointer must be released by free() after use.
char* getCameraName(IMoniker* moniker)
{
  LPOLESTR name;
  if (FAILED(moniker->GetDisplayName(nullptr, nullptr, &name)))
    return nullptr;

  std::string nameStr = utf16Decode(name);
  char* ret = (char*)malloc(nameStr.size() + 1);
  memcpy(ret, nameStr.c_str(), nameStr.size() + 1);

  LPMALLOC comalloc;
  CoGetMalloc(1, &comalloc);
  comalloc->Free(name);
  comalloc->Release();

  return ret;
}

// getDeviceFriendlyName returns the human-readable name of the device via IPropertyBag.
// Returns nullptr if the friendly name is not available.
// returned pointer must be released by free() after use.
static char* getDeviceFriendlyName(IMoniker* moniker)
{
  IPropertyBag* propBag = nullptr;
  if (FAILED(moniker->BindToStorage(0, 0, IID_IPropertyBag, (void**)&propBag)))
    return nullptr;

  VARIANT varName;
  VariantInit(&varName);
  if (FAILED(propBag->Read(L"FriendlyName", &varName, 0)))
  {
    VariantClear(&varName);
    propBag->Release();
    return nullptr;
  }

  if (varName.vt != VT_BSTR || varName.bstrVal == nullptr)
  {
    VariantClear(&varName);
    propBag->Release();
    return nullptr;
  }

  std::string nameStr = utf16Decode(varName.bstrVal);
  VariantClear(&varName);
  propBag->Release();

  char* ret = (char*)malloc(nameStr.size() + 1);
  memcpy(ret, nameStr.c_str(), nameStr.size() + 1);
  return ret;
}

// listCamera stores information of the devices to cameraList*.
int listCamera(cameraList* list, const char** errstr)
{
  ICreateDevEnum* sysDevEnum = nullptr;
  IEnumMoniker* enumMon = nullptr;

  if (FAILED(CoCreateInstance(
          CLSID_SystemDeviceEnum, nullptr, CLSCTX_INPROC,
          IID_ICreateDevEnum, (void**)&sysDevEnum)))
  {
    *errstr = errEnumDevice;
    goto fail;
  }

  if (FAILED(sysDevEnum->CreateClassEnumerator(
          CLSID_VideoInputDeviceCategory, &enumMon, 0)))
  {
    *errstr = errEnumDevice;
    goto fail;
  }

  safeRelease(&sysDevEnum);

  if (enumMon == nullptr)
  {
    list->num = 0;
    list->name = nullptr;
    list->friendlyName = nullptr;
    return 0;
  }

  {
    IMoniker* moniker;
    list->num = 0;
    while (enumMon->Next(1, &moniker, nullptr) == S_OK)
    {
      moniker->Release();
      list->num++;
    }

    enumMon->Reset();
    list->name = new char*[list->num]();
    list->friendlyName = new char*[list->num]();

    int i = 0;
    while (enumMon->Next(1, &moniker, nullptr) == S_OK)
    {
      list->name[i] = getCameraName(moniker);
      list->friendlyName[i] = getDeviceFriendlyName(moniker);
      moniker->Release();
      i++;
    }
    list->num = i;
  }

  safeRelease(&enumMon);
  return 0;

fail:
  safeRelease(&sysDevEnum);
  safeRelease(&enumMon);
  return 1;
}

// freeCameraList frees all resources stored in cameraList*.
int freeCameraList(cameraList* list, const char** errstr)
{
  if (list->name != nullptr)
  {
    for (int i = 0; i < list->num; ++i)
    {
      free(list->name[i]);
    }
    delete[] list->name;
  }
  if (list->friendlyName != nullptr)
  {
    for (int i = 0; i < list->num; ++i)
    {
      free(list->friendlyName[i]);
    }
    delete[] list->friendlyName;
  }
  return 1;
}

// selectCamera stores pointer to the selected device IMoniker* according to the configs in camera*.
int selectCamera(camera* cam, IMoniker** monikerSelected, const char** errstr)
{
  ICreateDevEnum* sysDevEnum = nullptr;
  IEnumMoniker* enumMon = nullptr;

  if (FAILED(CoCreateInstance(
          CLSID_SystemDeviceEnum, nullptr, CLSCTX_INPROC,
          IID_ICreateDevEnum, (void**)&sysDevEnum)))
  {
    *errstr = errEnumDevice;
    goto fail;
  }

  if (FAILED(sysDevEnum->CreateClassEnumerator(
          CLSID_VideoInputDeviceCategory, &enumMon, 0)))
  {
    *errstr = errEnumDevice;
    goto fail;
  }

  safeRelease(&sysDevEnum);

  if (enumMon == nullptr)
  {
    *errstr = errEnumDevice;
    return 0;
  }

  {
    IMoniker* moniker;
    while (enumMon->Next(1, &moniker, nullptr) == S_OK)
    {
      char* name = getCameraName(moniker);
      if (strcmp(cam->name, name) != 0)
      {
        free(name);
        safeRelease(&moniker);
        continue;
      }
      free(name);
      *monikerSelected = moniker;
      safeRelease(&enumMon);
      return 1;
    }
  }

  safeRelease(&enumMon);
  return 0;

fail:
  safeRelease(&sysDevEnum);
  safeRelease(&enumMon);
  return 0;
}

// listResolution stores list of the device to camera*.
int listResolution(camera* cam, const char** errstr)
{
  cam->props = nullptr;

  IMoniker* moniker = nullptr;
  IBaseFilter* captureFilter = nullptr;
  ICaptureGraphBuilder2* captureGraph = nullptr;
  IAMStreamConfig* config = nullptr;
  IPin* src = nullptr;

  if (!selectCamera(cam, &moniker, errstr))
  {
    goto fail;
  }

  if (!moniker || FAILED(moniker->BindToObject(0, 0, IID_IBaseFilter, (void**)&captureFilter))) { *errstr = errEnumDevice; goto fail; }
  safeRelease(&moniker);

  src = getPin(captureFilter, PINDIR_OUTPUT);
  if (src == nullptr)
  {
    *errstr = errGetConfig;
    goto fail;
  }

  // Getting IAMStreamConfig is stub on Wine. Requires real Windows.
  if (FAILED(src->QueryInterface(
          IID_IAMStreamConfig, (void**)&config)))
  {
    *errstr = errGetConfig;
    goto fail;
  }
  safeRelease(&src);

  {
    int count = 0, size = 0;
    if (FAILED(config->GetNumberOfCapabilities(&count, &size)) || count < 0 || count > 4096 || size <= 0 || size > 65536)
    {
      *errstr = errGetConfig;
      goto fail;
    }
    cam->props = new imageProp[count];
    std::vector<BYTE> caps(size);

    int iProp = 0;
    for (int i = 0; i < count; ++i)
    {
      AM_MEDIA_TYPE* mediaType = nullptr;
      if (FAILED(config->GetStreamCaps(i, &mediaType, caps.data())))
        continue;

      if (!mediaType || mediaType->majortype != MEDIATYPE_Video ||
          mediaType->pbFormat == nullptr)
      {
        freeMediaType(mediaType);
        continue;
      }

      BITMAPINFOHEADER* bmi = nullptr;
      if (mediaType->formattype == FORMAT_VideoInfo && mediaType->cbFormat >= sizeof(VIDEOINFOHEADER))
      {
        bmi = &((VIDEOINFOHEADER*)mediaType->pbFormat)->bmiHeader;
      }
      else if (mediaType->formattype == FORMAT_VideoInfo2 && mediaType->cbFormat >= sizeof(VIDEOINFOHEADER2))
      {
        bmi = &((VIDEOINFOHEADER2*)mediaType->pbFormat)->bmiHeader;
      }
      else
      {
        freeMediaType(mediaType);
        continue;
      }

      if (bmi->biWidth <= 0 || bmi->biWidth > 8192 || bmi->biHeight < -8192 || bmi->biHeight > 8192 || bmi->biHeight == 0) {freeMediaType(mediaType);continue;}
      cam->props[iProp].width = bmi->biWidth;
      cam->props[iProp].height = abs(bmi->biHeight);
      auto interval = mediaType->formattype == FORMAT_VideoInfo ? ((VIDEOINFOHEADER*)mediaType->pbFormat)->AvgTimePerFrame : ((VIDEOINFOHEADER2*)mediaType->pbFormat)->AvgTimePerFrame;
      cam->props[iProp].fps = interval > 0 ? 10000000.0 / interval : 0;
      // Use subtype.Data1 for the FourCC; some drivers leave biCompression as 0.
      cam->props[iProp].fcc = mediaType->subtype.Data1;
      freeMediaType(mediaType);
      iProp++;
    }
    cam->numProps = iProp;
  }
  safeRelease(&config);
  safeRelease(&captureGraph);
  safeRelease(&captureFilter);
  safeRelease(&moniker);
  return 0;

fail:
  safeRelease(&src);
  safeRelease(&config);
  safeRelease(&captureGraph);
  safeRelease(&captureFilter);
  safeRelease(&moniker);
  return 1;
}

// openCamera opens a camera and stores interface handler to camera*.
// camera* should be freed by freeCamera() after use.
int openCamera(camera* cam, const char** errstr)
{
  cam->grabber = nullptr;
  cam->mediaControl = nullptr;
  cam->callback = nullptr;

  IMoniker* moniker = nullptr;
  IGraphBuilder* graphBuilder = nullptr;
  IBaseFilter* captureFilter = nullptr;
  IMediaControl* mediaControl = nullptr;
  IBaseFilter* grabberFilter = nullptr;
  ISampleGrabber* grabber = nullptr;
  IBaseFilter* nullFilter = nullptr;
  IPin* src = nullptr;
  IPin* dst = nullptr;
  IPin* end = nullptr;
  IPin* nul = nullptr;

  if (!selectCamera(cam, &moniker, errstr))
  {
    goto fail;
  }
  if (!moniker || FAILED(moniker->BindToObject(0, 0, IID_IBaseFilter, (void**)&captureFilter))) { *errstr = errEnumDevice; goto fail; }
  safeRelease(&moniker);

  if (FAILED(CoCreateInstance(
          CLSID_FilterGraph, nullptr, CLSCTX_INPROC,
          IID_IGraphBuilder, (void**)&graphBuilder)))
  {
    *errstr = errGraphBuilder;
    goto fail;
  }

  if (FAILED(graphBuilder->QueryInterface(
          IID_IMediaControl, (void**)&mediaControl)))
  {
    *errstr = errNoControl;
    goto fail;
  }

  if (FAILED(graphBuilder->AddFilter(captureFilter, L"capture")))
  {
    *errstr = errAddFilter;
    goto fail;
  }

  if (FAILED(CoCreateInstance(
          CLSID_SampleGrabber, nullptr, CLSCTX_INPROC,
          IID_IBaseFilter, (void**)&grabberFilter)))
  {
    *errstr = errGrabber;
    goto fail;
  }

  if (FAILED(grabberFilter->QueryInterface(IID_ISampleGrabber, (void**)&grabber)))
  {
    *errstr = errGrabber;
    goto fail;
  }

  {
    IPin* output = getPin(captureFilter, PINDIR_OUTPUT);
    IAMStreamConfig* config = nullptr;
    if (!output || FAILED(output->QueryInterface(IID_IAMStreamConfig, (void**)&config))) {
      safeRelease(&output); *errstr = errGetConfig; goto fail;
    }
    safeRelease(&output);
    int count=0, size=0;
    HRESULT selected = E_FAIL;
    if (SUCCEEDED(config->GetNumberOfCapabilities(&count,&size)) && count>=0 && count<=4096 && size>0 && size<65536) {
      BYTE* caps = new BYTE[size];
      for (int i=0;i<count;i++) {
        AM_MEDIA_TYPE* mt=nullptr;
        if (FAILED(config->GetStreamCaps(i,&mt,caps))) continue;
        BITMAPINFOHEADER* bmi=nullptr; REFERENCE_TIME* interval=nullptr;
        if (mt && mt->pbFormat && mt->formattype==FORMAT_VideoInfo && mt->cbFormat>=sizeof(VIDEOINFOHEADER)) { auto v=(VIDEOINFOHEADER*)mt->pbFormat; bmi=&v->bmiHeader; interval=&v->AvgTimePerFrame; }
        else if (mt && mt->pbFormat && mt->formattype==FORMAT_VideoInfo2 && mt->cbFormat>=sizeof(VIDEOINFOHEADER2)) { auto v=(VIDEOINFOHEADER2*)mt->pbFormat; bmi=&v->bmiHeader; interval=&v->AvgTimePerFrame; }
        if (bmi && bmi->biWidth==cam->width && abs(bmi->biHeight)==cam->height && mt->subtype.Data1==cam->fcc) {
          if (cam->fps>0) *interval=(REFERENCE_TIME)(10000000.0/cam->fps);
          selected=config->SetFormat(mt);
        }
        freeMediaType(mt);
        if (SUCCEEDED(selected)) break;
      }
      delete[] caps;
    }
    safeRelease(&config);
    if (FAILED(selected)) { *errstr=errGetConfig; goto fail; }
  }

  {
    AM_MEDIA_TYPE mediaType;
    memset(&mediaType, 0, sizeof(mediaType));
    mediaType.majortype = MEDIATYPE_Video;
    if (cam->fcc == FOURCC_NV12)
      mediaType.subtype = MEDIASUBTYPE_NV12;
    else
      mediaType.subtype = MEDIASUBTYPE_YUY2;
    // formattype left as GUID_NULL (wildcard) - accepts both VideoInfo and VideoInfo2
    if (FAILED(grabber->SetMediaType(&mediaType)))
    {
      *errstr = errGrabber;
      goto fail;
    }
  }

  if (FAILED(graphBuilder->AddFilter(grabberFilter, L"grabber")))
  {
    *errstr = errAddFilter;
    goto fail;
  }

  if (FAILED(CoCreateInstance(
          CLSID_NullRenderer, nullptr, CLSCTX_INPROC,
          IID_IBaseFilter, (void**)&nullFilter)))
  {
    *errstr = errTerminator;
    goto fail;
  }

  if (FAILED(graphBuilder->AddFilter(nullFilter, L"bull")))
  {
    *errstr = errAddFilter;
    goto fail;
  }

  HRESULT hr;
  src = getPin(captureFilter, PINDIR_OUTPUT);
  dst = getPin(grabberFilter, PINDIR_INPUT);
  if (src == nullptr || dst == nullptr ||
      FAILED(hr = graphBuilder->Connect(src, dst)))
  {
    *errstr = errConnectFilters;
    goto fail;
  }

  safeRelease(&src);
  safeRelease(&dst);

  end = getPin(grabberFilter, PINDIR_OUTPUT);
  nul = getPin(nullFilter, PINDIR_INPUT);
  if (end == nullptr || nul == nullptr ||
      FAILED(hr = graphBuilder->Connect(end, nul)))
  {
    *errstr = errConnectFilters;
    goto fail;
  }

  safeRelease(&end);
  safeRelease(&nul);

  {
    // This graph captures samples; it has no playback presentation timeline.
    // A renderer clock can hold subsequent live samples (observed as a
    // first-sample-only stall with an already running OBS input). Consume
    // arrivals immediately; the source still owns its cadence.
    IMediaFilter* graphFilter = nullptr;
    if (FAILED(graphBuilder->QueryInterface(IID_IMediaFilter, (void**)&graphFilter)))
    {
      *errstr = errNoControl;
      goto fail;
    }
    HRESULT clockResult = graphFilter->SetSyncSource(nullptr);
    safeRelease(&graphFilter);
    if (FAILED(clockResult))
    {
      *errstr = errNoControl;
      goto fail;
    }
  }

  safeRelease(&nullFilter);
  safeRelease(&captureFilter);
  safeRelease(&grabberFilter);
  safeRelease(&graphBuilder);

  {
    SampleGrabberCallback* cb = new SampleGrabberCallback(cam);
    grabber->SetCallback(cb, 1);
    cam->grabber = (void*)grabber;
    cam->mediaControl = (void*)mediaControl;
    cam->callback = (void*)cb;

    grabber->SetBufferSamples(true);
    if (FAILED(mediaControl->Run())) { freeCamera(cam); *errstr = errNoControl; return 1; }
  }

  return 0;

fail:
  safeRelease(&src);
  safeRelease(&dst);
  safeRelease(&end);
  safeRelease(&nul);
  safeRelease(&nullFilter);
  safeRelease(&grabber);
  safeRelease(&grabberFilter);
  safeRelease(&mediaControl);
  safeRelease(&captureFilter);
  safeRelease(&graphBuilder);
  safeRelease(&moniker);
  return 1;
}

// SampleCB is not used in this app.
HRESULT SampleGrabberCallback::SampleCB(double sampleTime, IMediaSample* sample)
{
  return S_OK;
}

// BufferCB receives image from DirectShow.
HRESULT SampleGrabberCallback::BufferCB(double sampleTime, BYTE* buf, LONG len)
{
  BYTE* gobuf = (BYTE*)cam_->buf;
  const int nPix = cam_->width * cam_->height;
  const int expected = cam_->fcc == FOURCC_NV12 ? nPix * 3 / 2 : nPix * 2;
  if (len != expected)
  {
    fprintf(stderr, "Wrong frame buffer size: %d > %d\n", len, nPix * 2);
    return S_OK;
  }

  if (cam_->fcc == FOURCC_NV12)
  {
    // NV12: Y plane (nPix bytes) + interleaved UV plane (nPix/2 bytes).
    // Convert to I420 planar: Y + U + V separate planes.
    memcpy(gobuf, buf, nPix);
    BYTE* uv = buf + nPix;
    int ui = nPix;
    int vi = nPix + nPix / 4;
    for (int i = 0; i < nPix / 2; i += 2)
    {
      gobuf[ui++] = uv[i];
      gobuf[vi++] = uv[i + 1];
    }
  }
  else
  {
    // YUY2: packed YUYV. Convert to I422 planar.
    int yi = 0;
    int cbi = nPix;
    int cri = cbi + cbi / 2;
    for (int y = 0; y < cam_->height; ++y)
    {
      int j = y * cam_->width * 2;
      for (int x = 0; x < cam_->width / 2; ++x)
      {
        gobuf[yi] = buf[j];
        gobuf[cbi] = buf[j + 1];
        gobuf[yi + 1] = buf[j + 2];
        gobuf[cri] = buf[j + 3];
        j += 4;
        yi += 2;
        cbi++;
        cri++;
      }
    }
  }

  imageCallback((size_t)cam_);
  return S_OK;
}

// freeCamera closes device and frees all resources allocated by openCamera().
void freeCamera(camera* cam)
{
  if (cam->mediaControl)
    ((IMediaControl*)cam->mediaControl)->Stop();

  safeRelease((ISampleGrabber**)&cam->grabber);
  safeRelease((IMediaControl**)&cam->mediaControl);

  if (cam->callback)
  {
    ((SampleGrabberCallback*)cam->callback)->Release();
    delete ((SampleGrabberCallback*)cam->callback);
    cam->callback = nullptr;
  }

  if (cam->props)
  {
    delete[] cam->props;
    cam->props = nullptr;
  }
}

// utf16Decode returns UTF-8 string from UTF-16 string.
std::string utf16Decode(LPOLESTR olestr)
{
  std::wstring wstr(olestr);
  const int len = WideCharToMultiByte(
      CP_UTF8, 0,
      wstr.data(), (int)wstr.size(),
      nullptr, 0, nullptr, nullptr);
  std::string str(len, 0);
  WideCharToMultiByte(
      CP_UTF8, 0,
      wstr.data(), (int)wstr.size(),
      (LPSTR)str.data(), len, nullptr, nullptr);
  return str;
}

// getPin is a helper to get I/O pin of DirectShow filter.
IPin* getPin(IBaseFilter* filter, PIN_DIRECTION dir)
{
  IEnumPins* enumPins;
  if (FAILED(filter->EnumPins(&enumPins)))
    return nullptr;

  IPin* pin;
  while (enumPins->Next(1, &pin, nullptr) == S_OK)
  {
    PIN_DIRECTION d;
    pin->QueryDirection(&d);
    if (d == dir)
    {
      enumPins->Release();
      return pin;
    }
    pin->Release();
  }
  enumPins->Release();
  return nullptr;
}
