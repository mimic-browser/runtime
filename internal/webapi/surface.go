package webapi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/moreveal/mimic/compatibility"
)

// Surface is kept separate from semantic host implementations so it can be
// replaced by generated WebIDL bindings without changing browser services.
//
//go:embed locale.js
var localeSurface string

//go:embed intl.js
var intlSurface string

//go:embed trusted_types_sinks.js
var trustedTypesSinksSurface string

//go:embed dialog.js
var dialogSurface string

//go:embed trusted_types.js
var trustedTypesSurface string

//go:embed surface.js
var handwrittenSurface string

//go:embed rtc_media.js
var rtcMediaSurface string

//go:embed camera_capture.js
var cameraCaptureSurface string

//go:embed rtc_session.js
var rtcSessionSurface string

//go:embed console.js
var consoleSurface string

//go:embed structured_clone.js
var structuredCloneSurface string

//go:embed performance.js
var performanceSurface string

//go:embed performance_interfaces.json
var performanceInterfaces string

func performanceSource() string {
	return strings.Replace(performanceSurface, "/* performance_interface_schema */", performanceInterfaces, 1)
}

//go:embed base64.js
var base64Surface string

//go:embed webcrypto.js
var webCryptoSurface string

//go:embed css_registered_properties.js
var cssRegisteredPropertiesSurface string

//go:embed css_property_worlds.js
var cssPropertyWorldsSurface string

//go:embed css_typed_values.js
var cssTypedValuesSurface string

//go:embed storage_events.js
var storageEventsSurface string

//go:embed view_transitions.js
var viewTransitionsSurface string

//go:embed native_functions.js
var nativeFunctionsSurface string

//go:embed dom_name_validation.js
var domNameValidationSurface string

//go:embed webkit_css_names.js
var webkitCSSNamesSurface string

//go:embed css_property_catalog.js
var cssPropertyCatalogSurface string

//go:embed css_media_queries.js
var cssMediaQueriesSurface string

//go:embed css_observable_values.js
var cssObservableValuesSurface string

//go:embed css_computed_values.js
var cssComputedValuesSurface string

//go:embed webkit_css.js
var webkitCSSSurface string

//go:embed css_supports.js
var cssSupportsSurface string

//go:embed css_shorthands.js
var cssShorthandsSurface string

//go:embed css_value_grammar.js
var cssValueGrammarSurface string

//go:embed css_font_metrics.js
var cssFontMetricsSurface string

//go:embed css_animation_grammar.js
var cssAnimationGrammarSurface string

//go:embed worker.js
var handwrittenWorkerSurface string

//go:embed capabilities.js
var capabilitySurface string

//go:embed media_source.js
var mediaSourceSurface string

//go:embed dom_compatibility.js
var domCompatibilitySurface string

//go:embed templates_compatibility.js
var templatesCompatibilitySurface string

//go:embed shadow_serialization.js
var shadowSerializationSurface string

//go:embed cssom_compatibility.js
var cssomCompatibilitySurface string

//go:embed intersection_observer.js
var intersectionObserverSurface string

//go:embed traversal_compatibility.js
var traversalCompatibilitySurface string

//go:embed document_compatibility.js
var documentCompatibilitySurface string

//go:embed document_all.js
var documentAllSurface string

//go:embed attributes_compatibility.js
var attributesCompatibilitySurface string

//go:embed web_animations.js
var webAnimationsSurface string

//go:embed svg_geometry.js
var svgGeometrySurface string

//go:embed svg_text.js
var svgTextSurface string

//go:embed svg_boundaries.js
var svgBoundariesSurface string

//go:embed svg_types.js
var svgTypesSurface string

//go:embed svg_coordinates.js
var svgCoordinatesSurface string

//go:embed svg_path_metrics.js
var svgPathMetricsSurface string

//go:embed svg_use.js
var svgUseSurface string

//go:embed svg_attribute_defaults.js
var svgAttributeDefaultsSurface string

//go:embed svg_attribute_semantics.js
var svgAttributeSemanticsSurface string

//go:embed svg_reflections.js
var svgReflectionsSurface string

//go:embed svg_css_transform.js
var svgCSSTransformSurface string

//go:embed window_observations.js
var windowObservationsSurface string

//go:embed document_picture_in_picture.js
var documentPictureInPictureSurface string

//go:embed window_services.js
var windowServicesSurface string

//go:embed speech_synthesis.js
var speechSynthesisSurface string

//go:embed navigation.js
var navigationSurface string

//go:embed indexeddb.js
var indexedDBSurface string

//go:embed cache_storage.js
var cacheStorageSurface string

//go:embed events_compatibility.js
var eventsCompatibilitySurface string

//go:embed window_errors.js
var windowErrorsSurface string

//go:embed input.js
var inputSurface string

//go:embed scrolling.js
var scrollingSurface string

//go:embed document_stream.js
var documentStreamSurface string

//go:embed selectors_vendor.js
var selectorsVendorSurface string

//go:embed selectors_compatibility.js
var selectorsCompatibilitySurface string

//go:embed xpath_compatibility.js
var xpathCompatibilitySurface string

//go:embed streams_vendor.js
var streamsVendorSurface string

//go:embed fetch_compatibility.js
var fetchCompatibilitySurface string

//go:embed file_system.js
var fileSystemSurface string

//go:embed fetch_primitives.js
var fetchPrimitivesSurface string

//go:embed promise_intrinsics.js
var promiseIntrinsicsSurface string

//go:embed abort_encoding.js
var abortEncodingSurface string

//go:embed canvas_path_observations.js
var canvasPathObservationsSurface string

//go:embed canvas_state.js
var canvasStateSurface string

//go:embed canvas_color_spaces.js
var canvasColorSpacesSurface string

func canvasObservationsSource() string {
	s := strings.Replace(canvasStateSurface, "/* shared_canvas_colors */", canvasColorSpacesSurface, 1)
	return strings.Replace(s, "/* shared_canvas_paths */", canvasPathObservationsSurface, 1)
}

// LazyDomainSource keeps dormant implementation source in process-wide Go
// storage rather than copying it into every realm's V8 heap. Generated WebIDL
// bindings still publish the complete observable shape during bootstrap.
func LazyDomainSource(name string) (string, bool) {
	switch name {
	case "audio":
		return strings.Replace(strings.Replace(offlineAudioSurface, "/* shared_audio_nodes */", audioNodesSurface, 1), "/* shared_realtime_audio */", realtimeAudioSurface, 1), true
	case "webgl":
		return webglObservationsSource(), true
	case "webgpu":
		return webgpuObservationsSource(), true
	default:
		return "", false
	}
}

//go:embed glsl_observations.js
var glslObservationsSurface string

//go:embed webgl_framebuffer_observations.js
var webglFramebufferObservationsSurface string

//go:embed webgl_extensions.js
var webglExtensionsSurface string

//go:embed webgl_texture_observations.js
var webglTextureObservationsSurface string

//go:embed webgl_vertex_arrays.js
var webglVertexArraysSurface string

//go:embed webgl_program_observations.js
var webglProgramObservationsSurface string

func webglObservationsSource() string {
	return strings.NewReplacer("/* shared_glsl_observations */", glslObservationsSurface, "/* shared_webgl_extensions */", webglExtensionsSurface, "/* shared_webgl_textures */", webglTextureObservationsSurface, "/* shared_webgl_vertex_arrays */", webglVertexArraysSurface, "/* shared_webgl_programs */", webglProgramObservationsSurface, "/* shared_webgl_framebuffers */", webglFramebufferObservationsSurface).Replace(webglStateSurface)
}

//go:embed offline_audio.js
var offlineAudioSurface string

//go:embed font_faces.js
var fontFacesSurface string

//go:embed webgpu_state.js
var webgpuStateSurface string

//go:embed wgsl_observations.js
var wgslObservationsSurface string

//go:embed webgpu_render_observations.js
var webgpuRenderObservationsSurface string

func webgpuObservationsSource() string {
	return strings.NewReplacer("/* shared_glsl_observations */", glslObservationsSurface, "/* shared_wgsl_observations */", wgslObservationsSurface, "/* shared_webgpu_render */", webgpuRenderObservationsSurface).Replace(webgpuStateSurface)
}

//go:embed webgl_state.js
var webglStateSurface string

//go:embed document_state.js
var documentStateSurface string

//go:embed image_resources.js
var imageResourcesSurface string

//go:embed screen_focus.js
var screenFocusSurface string

//go:embed worker_storage.js
var workerStorageSurface string

//go:embed audio_nodes.js
var audioNodesSurface string

//go:embed realtime_audio.js
var realtimeAudioSurface string

//go:embed navigator_identity.js
var navigatorIdentitySurface string

//go:embed css_colors.js
var cssColorsSurface string

//go:embed css_box_geometry.js
var cssBoxGeometrySurface string

//go:embed element_visibility.js
var elementVisibilitySurface string

//go:embed dom_matrix.js
var domMatrixSurface string

//go:embed form_controls.js
var formControlsSurface string

//go:embed constraint_validation.js
var constraintValidationSurface string

type surfaceKey struct {
	surface  *compatibility.WebAPISurface
	exposure string
}

type surfaceOutput struct {
	Source, ExposureJSON, CatalogJSON string
	Digest                            [32]byte
}

var surfaceSources = struct {
	sync.Mutex
	values map[surfaceKey]func() surfaceOutput
}{values: map[surfaceKey]func() surfaceOutput{}}

// SurfaceFor shares only immutable source text from a compatibility bundle.
// The bounded cache owns no Page, isolate, or mutable JavaScript object.
func SurfaceFor(surface *compatibility.WebAPISurface, name string) (string, string) {
	output := bootstrapFor(surface, name)
	return output.Source, output.ExposureJSON
}

// BootstrapFor shares only immutable, selected-profile data across Pages.
func BootstrapFor(surface *compatibility.WebAPISurface, name string) (string, string, string) {
	output := bootstrapFor(surface, name)
	return output.Source, output.ExposureJSON, output.CatalogJSON
}

// BootstrapDigest hashes immutable transport once. Browser profile inputs are
// still included separately in each document's artifact key.
func BootstrapDigest(surface *compatibility.WebAPISurface, name string) [32]byte {
	return bootstrapFor(surface, name).Digest
}

func bootstrapFor(surface *compatibility.WebAPISurface, name string) surfaceOutput {
	key := surfaceKey{surface, name}
	return cachedSurface(key, func() surfaceOutput {
		if metadata, ok := surface.BootstrapMetadata[name]; ok {
			return surfaceOutput{Source: composeSurface(surface.GeneratedJavaScript, "applyTargetExposure(JSON.parse(host.exposureJSON()));\n"), ExposureJSON: metadata.ExposureJSON, CatalogJSON: metadata.CatalogJSON}
		}
		exposure, ok := surface.Exposure(name)
		if !ok {
			return surfaceOutput{Source: Surface(surface.GeneratedJavaScript, nil), CatalogJSON: surface.GeneratedCatalogJSON}
		}
		encoded, err := marshalExposure(exposure)
		if err != nil {
			panic(err)
		}
		return surfaceOutput{Source: composeSurface(surface.GeneratedJavaScript, "applyTargetExposure(JSON.parse(host.exposureJSON()));\n"), ExposureJSON: string(encoded), CatalogJSON: selectedCatalog(surface.GeneratedCatalogJSON, exposure)}
	})
}

// WorkerSurfaceFor caches immutable source composition, never agent state.
func WorkerSurfaceFor(surface *compatibility.WebAPISurface, name string) string {
	return cachedSurface(surfaceKey{surface, "worker:" + name}, func() surfaceOutput {
		exposure, ok := surface.Exposure(name)
		if !ok {
			return surfaceOutput{Source: WorkerSurface(surface.GeneratedJavaScript, nil)}
		}
		return surfaceOutput{Source: WorkerSurface(surface.GeneratedJavaScript, &exposure)}
	}).Source
}

func cachedSurface(key surfaceKey, compose func() surfaceOutput) surfaceOutput {
	surfaceSources.Lock()
	build := surfaceSources.values[key]
	if build == nil {
		build = sync.OnceValue(func() surfaceOutput {
			output := compose()
			hash := sha256.New()
			for _, part := range []string{output.Source, output.ExposureJSON, output.CatalogJSON} {
				io.Copy(hash, strings.NewReader(part))
				hash.Write([]byte{0})
			}
			copy(output.Digest[:], hash.Sum(nil))
			return output
		})
		if len(surfaceSources.values) < 8 {
			surfaceSources.values[key] = build
		}
	}
	surfaceSources.Unlock()
	return build()
}
func Surface(generated string, exposure *compatibility.RealmExposure) string {
	return buildSurface(generated, exposure)
}
func buildSurface(generated string, exposure *compatibility.RealmExposure) string {
	// Tracking starts only after both handwritten semantics and the selected
	// bundle's generated surface have been installed. Otherwise feature
	// detection performed by the generator pollutes runtime API traces.
	exposureSource := ""
	if exposure != nil {
		encoded, err := json.Marshal(exposure)
		if err != nil {
			panic(err)
		}
		quoted, _ := json.Marshal(string(encoded))
		exposureSource = "applyTargetExposure(JSON.parse(" + string(quoted) + "));\n"
	}
	return composeSurface(generated, exposureSource)
}

func composeSurface(generated, exposureSource string) string {
	// Use a formatter-stable sentinel instead of depending on authored JS whitespace.
	marker := "/* compose_surface_tail */"
	// Generated shape alone does not establish transferable structured cloning.
	// Keep the upstream stream bundle unchanged, and select only a functioning
	// transfer primitive for its fallback path (engines without one copy bytes).
	streamPrelude := `{let structuredClone;try{const input=new ArrayBuffer(1),clone=globalThis.structuredClone;if(typeof clone==='function'){const output=clone(input,{transfer:[input]});if(output instanceof ArrayBuffer&&output.byteLength===1&&input.byteLength===0)structuredClone=clone}}catch{}`
	domCompatibility := strings.Replace(domCompatibilitySurface, "/* dialog_lifecycle */", dialogSurface, 1)
	parts := []string{capabilitySurface, generated, "installLazyShape();", "finalizeBindings();", exposureSource, "installGeneratedLazyOperations();",
		"installNavigatorCapabilities();installLazySingletonBindings();", mediaSourceSurface, navigatorIdentitySurface, cssSupportsSurface, strings.Replace(domCompatibility, "/* shared_abort_encoding */", abortEncodingSurface, 1),
		templatesCompatibilitySurface, selectorsVendorSurface, selectorsCompatibilitySurface, xpathCompatibilitySurface, cssomCompatibilitySurface, cssRegisteredPropertiesSurface, cssPropertyWorldsSurface, cssTypedValuesSurface, strings.Replace(strings.Replace(strings.Replace(strings.Replace(svgGeometrySurface, "/* shared_svg_boundaries */", svgBoundariesSurface, 1), "/* shared_svg_text */", svgTextSurface, 1), "/* shared_svg_css_transform */", svgCSSTransformSurface, 1), "/* shared_svg_types */", svgTypesSurface+svgCoordinatesSurface+svgReflectionsSurface+svgPathMetricsSurface+svgUseSurface+svgAttributeDefaultsSurface+svgAttributeSemanticsSurface, 1), streamPrelude,
		streamsVendorSurface, "}", strings.Replace(fetchCompatibilitySurface, "/* cache_storage */", cacheStorageSurface, 1), strings.Replace(formControlsSurface, "/* constraint_validation */", oneByteSource(constraintValidationSurface), 1), traversalCompatibilitySurface, strings.Replace(documentCompatibilitySurface, "/* shared_document_state */", documentStateSurface, 1), documentAllSurface, attributesCompatibilitySurface, webAnimationsSurface, imageResourcesSurface, screenFocusSurface, eventsCompatibilitySurface, storageEventsSurface, viewTransitionsSurface, windowErrorsSurface, inputSurface, windowObservationsSurface, windowServicesSurface, speechSynthesisSurface, documentPictureInPictureSurface, navigationSurface, indexedDBSurface, fileSystemSurface, documentStreamSurface, shadowSerializationSurface, canvasObservationsSource(), fontFacesSurface, rtcSessionSurface, cameraCaptureSurface, rtcMediaSurface, trustedTypesSinksSurface, "trustedCaptureEvents();registerBootstrapCallback('installTrustedTypesEnforcer',trustedEnforceString);", "finalizeDocumentGetterBindings();finalizeSingletonGetterBindings();finalizePerformanceBindings();finalizeCallableBindings();finalizeNativeBindings();globalThis.__mimicNativeFunctionSources=nativeFunctionSourceState;", "finalizeFrameIntrinsicBindings();", marker}
	base := strings.Replace(oneByteSource(handwrittenSurface), "/* shared_fetch_primitives */", fetchPrimitivesSurface, 1)
	base = strings.Replace(base, "/* shared_promise_intrinsics */", promiseIntrinsicsSurface, 1)
	base = strings.Replace(base, "/* profile_locale */", localeSurface, 1)
	base = strings.Replace(base, "/* native_intl */", intlSurface, 1)

	base = strings.Replace(base, "/* shared_console */", consoleSurface, 1)
	base = strings.Replace(base, "/* shared_trusted_types */", trustedTypesSurface, 1)
	base = strings.Replace(base, "/* shared_structured_clone */", structuredCloneSurface, 1)
	base = strings.Replace(base, "/* shared_performance */", performanceSource(), 1)
	base = strings.Replace(base, "/* shared_native_functions */", nativeFunctionsSurface, 1)
	base = strings.Replace(base, "/* shared_dom_name_validation */", domNameValidationSurface, 1)
	base = strings.Replace(base, "/* shared_base64 */", base64Surface, 1)
	base = strings.Replace(base, "/* shared_webcrypto */", webCryptoSurface, 1)
	base = strings.Replace(base, "/* shared_webkit_css */", webkitCSSNamesSurface+cssPropertyCatalogSurface+cssComputedValuesSurface+webkitCSSSurface+cssShorthandsSurface+cssValueGrammarSurface+cssAnimationGrammarSurface+cssFontMetricsSurface+cssObservableValuesSurface+cssMediaQueriesSurface, 1)
	base = strings.Replace(base, "/* shared_css_box_geometry */", cssBoxGeometrySurface+elementVisibilitySurface, 1)
	base = strings.Replace(base, "/* shared_dom_matrix */", cssColorsSurface+domMatrixSurface, 1)
	base = strings.Replace(base, "/* shared_intersection_observer */", intersectionObserverSurface, 1)
	base = strings.Replace(base, marker, strings.Join(parts, "\n"), 1)
	base = strings.Replace(base, "/* shared_scrolling */", scrollingSurface, 1)
	return base
}

// Keep the authored source readable while allowing V8 to retain the large
// composed bootstrap source as a one-byte string. The non-Latin-1 characters
// in these two modules occur only in string literals and comments.
func oneByteSource(source string) string {
	var out strings.Builder
	out.Grow(len(source))
	for _, char := range source {
		if char <= 0xff || char > 0xffff {
			out.WriteRune(char)
			continue
		}
		out.WriteString(`\u`)
		hex := strconv.FormatInt(int64(char), 16)
		for pad := 4 - len(hex); pad > 0; pad-- {
			out.WriteByte('0')
		}
		out.WriteString(hex)
	}
	return out.String()
}

func WorkerSurface(generated string, exposure *compatibility.RealmExposure) string {
	exposureSource := ""
	if exposure != nil {
		encoded, err := json.Marshal(exposure)
		if err != nil {
			panic(err)
		}
		exposureSource = "const __workerExposure=" + string(encoded) + ";__applyWorkerExposure(__workerExposure);\n"
	}
	// Initial Worker algorithms and the shared surface retain the same private
	// exception factory. The constructor itself stays in the shared surface scope.
	workerPrimitives := strings.Replace(fetchPrimitivesSurface, "const platformDOMException =", "platformDOMException =", 1)
	workerPrimitives = strings.Replace(workerPrimitives, "const bufferSourceBytes =", "bufferSourceBytes =", 1)
	shared := structuredCloneSurface + "\n" + workerPrimitives + "\ninstallFileReader();Object.assign(globalThis,{TextEncoder,DOMException,URL,URLSearchParams,Blob,File,FormData,FileReader,Headers});\n" + abortEncodingSurface + "\n{let structuredClone;\n" + streamsVendorSurface + "\n}\n" + fetchCompatibilitySurface
	// Preserve one private factory across the Worker's separate platform blocks.
	// The consuming block removes the transport property before author code.
	shared += "\nglobalThis.__workerPlatformDOMException=platformDOMException;\n"
	base := strings.Replace(strings.Replace(handwrittenWorkerSurface, "/* shared_trusted_types */", trustedTypesSurface, 1), "/* shared_worker_fetch */", shared, 1)
	base = strings.Replace(base, "/* shared_performance */", performanceSource(), 1)
	base = strings.Replace(base, "/* shared_promise_intrinsics */", promiseIntrinsicsSurface, 1)
	// These base64 functions are private WebCrypto helpers. Public Worker
	// operations are installed and marked in the final native binding scope.
	base = strings.Replace(base, "/* shared_webcrypto */", "const markNative = () => {};\n"+base64Surface+webCryptoSurface, 1)
	base += "\n{const host=__workerHost,bootstrapRestoreHooks=[];let intlEnvironment=host.intlEnvironment();const nativeIntl=typeof Intl!=='undefined';const bindingString=value=>{if(typeof value==='symbol')throw new TypeError('Cannot convert a Symbol value to a string');return String(value)};\n" + nativeFunctionsSurface + "\nif(nativeIntl){\n" + intlSurface + "\n}\n" + localeSurface + "\n}\n"
	return base + "\n" + generated + "\n" + exposureSource + "__finishWorkerSurface();if(typeof __workerExposure!=='undefined')__applyWorkerPrototypeExposure(__workerExposure);delete globalThis.__applyWorkerExposure;delete globalThis.__applyWorkerPrototypeExposure;delete globalThis.__finishWorkerSurface;delete globalThis.__mimic;delete globalThis.__mimicIDLExposure;\n{const host=__workerHost;\nconst platformDOMException=globalThis.__workerPlatformDOMException;\ndelete globalThis.__workerPlatformDOMException;\n" + promiseIntrinsicsSurface + nativeFunctionsSurface + navigatorIdentitySurface + workerStorageSurface + fileSystemSurface + "\ndelete globalThis.__mimicClonePlatforms;\n" + consoleSurface + "\nglobalThis.console=consoleObject;\n" + base64Surface + cssColorsSurface + "\nfor(const [name,value] of Object.entries({atob,btoa}))Object.defineProperty(WorkerGlobalScope.prototype,name,{value,writable:true,enumerable:true,configurable:true});\n" + domMatrixSurface + "\n" + canvasObservationsSource() + "\n" + webglObservationsSource() + "\n" + webgpuObservationsSource() + "\n" + fontFacesSurface + `
// Worker operations need the same ordinary function shape and private source
// registration as Window operations. This does not replace their implementations.
for(const owner of [globalThis.WorkerGlobalScope?.prototype,globalThis.DedicatedWorkerGlobalScope?.prototype]){
 if(!owner)continue;
 for(const key of Reflect.ownKeys(owner)){
  if(key==='constructor')continue;
  const d=Object.getOwnPropertyDescriptor(owner,key);
  if(typeof d.value==='function'){
   const original=d.value,name=String(key);
   if(Object.getOwnPropertyDescriptor(original,'prototype')||Object.getPrototypeOf(original)!==Function.prototype){
    d.value=({[name](...args){return functionSourceApply(original,this,args)}})[name];
    Object.defineProperty(d.value,'length',{value:original.length,configurable:true});
    Object.defineProperty(owner,key,d);
   }
   d.value=platformOperation(d.value,d.value.name||name);
   Object.defineProperty(owner,key,d);
   markNative(d.value,name);
  }
  for(const [kind,prefix] of [['get','get '],['set','set ']])if(typeof d[kind]==='function'){
   Object.defineProperty(d[kind],'name',{value:prefix+String(key),configurable:true});
   d[kind]=platformOperation(d[kind],prefix+String(key));
   Object.defineProperty(owner,key,d);
   markNative(d[kind],String(key),prefix);
  }
 }
}
globalThis.__mimicNativeFunctionSources=nativeFunctionSourceState;
}`
}
