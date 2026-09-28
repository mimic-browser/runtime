// External defines derived from the pinned patched v8 crate 152.2.0 GN build.
// Target OS and cppgc object-section support differ between the two hosts.
// Re-derive from v8_header_features and cppgc_header_features when rebuilding;
// do not infer ABI-affecting defaults from the upstream V8 repository.
// Required: pointer compression with multiple cages, sandbox off, checks off.
// Linux also sets v8_monolithic_for_shared_library=true.

// The following definitions were used when V8 itself was built, but also appear
// in the externally-visible header files and so must be included by any
// embedder. This will be done automatically if V8_GN_HEADER is defined.
// Ready-compiled distributions of V8 will need to provide this generated header
// along with the other headers in include.

// This header must be stand-alone because it is used across targets without
// introducing dependencies. It should only be included via v8config.h.

#ifndef V8_ARRAY_BUFFER_INTERNAL_FIELD_COUNT
#define V8_ARRAY_BUFFER_INTERNAL_FIELD_COUNT 2
#else
#if V8_ARRAY_BUFFER_INTERNAL_FIELD_COUNT != 2
#error "V8_ARRAY_BUFFER_INTERNAL_FIELD_COUNT defined but not set to 2"
#endif
#endif  // V8_ARRAY_BUFFER_INTERNAL_FIELD_COUNT

#ifndef V8_ARRAY_BUFFER_VIEW_INTERNAL_FIELD_COUNT
#define V8_ARRAY_BUFFER_VIEW_INTERNAL_FIELD_COUNT 2
#else
#if V8_ARRAY_BUFFER_VIEW_INTERNAL_FIELD_COUNT != 2
#error "V8_ARRAY_BUFFER_VIEW_INTERNAL_FIELD_COUNT defined but not set to 2"
#endif
#endif  // V8_ARRAY_BUFFER_VIEW_INTERNAL_FIELD_COUNT

#ifndef V8_PROMISE_INTERNAL_FIELD_COUNT
#define V8_PROMISE_INTERNAL_FIELD_COUNT 1
#else
#if V8_PROMISE_INTERNAL_FIELD_COUNT != 1
#error "V8_PROMISE_INTERNAL_FIELD_COUNT defined but not set to 1"
#endif
#endif  // V8_PROMISE_INTERNAL_FIELD_COUNT

#ifndef V8_USE_DEFAULT_HASHER_SECRET
#define V8_USE_DEFAULT_HASHER_SECRET false
#else
#if V8_USE_DEFAULT_HASHER_SECRET != false
#error "V8_USE_DEFAULT_HASHER_SECRET defined but not set to false"
#endif
#endif  // V8_USE_DEFAULT_HASHER_SECRET

#ifndef V8_COMPRESS_POINTERS
#define V8_COMPRESS_POINTERS 1
#else
#if V8_COMPRESS_POINTERS != 1
#error "V8_COMPRESS_POINTERS defined but not set to 1"
#endif
#endif  // V8_COMPRESS_POINTERS

#ifndef V8_31BIT_SMIS_ON_64BIT_ARCH
#define V8_31BIT_SMIS_ON_64BIT_ARCH 1
#else
#if V8_31BIT_SMIS_ON_64BIT_ARCH != 1
#error "V8_31BIT_SMIS_ON_64BIT_ARCH defined but not set to 1"
#endif
#endif  // V8_31BIT_SMIS_ON_64BIT_ARCH

#ifndef V8_DEPRECATION_WARNINGS
#define V8_DEPRECATION_WARNINGS 1
#else
#if V8_DEPRECATION_WARNINGS != 1
#error "V8_DEPRECATION_WARNINGS defined but not set to 1"
#endif
#endif  // V8_DEPRECATION_WARNINGS

#ifndef V8_CPPGC_MICROTASK_QUEUE
#define V8_CPPGC_MICROTASK_QUEUE 1
#else
#if V8_CPPGC_MICROTASK_QUEUE != 1
#error "V8_CPPGC_MICROTASK_QUEUE defined but not set to 1"
#endif
#endif  // V8_CPPGC_MICROTASK_QUEUE

#ifndef V8_HAVE_TARGET_OS
#define V8_HAVE_TARGET_OS 1
#else
#if V8_HAVE_TARGET_OS != 1
#error "V8_HAVE_TARGET_OS defined but not set to 1"
#endif
#endif  // V8_HAVE_TARGET_OS

#if defined(_WIN32)
#ifndef V8_TARGET_OS_WIN
#define V8_TARGET_OS_WIN 1
#elif V8_TARGET_OS_WIN != 1
#error "V8_TARGET_OS_WIN must be 1"
#endif
#elif defined(__linux__)
#ifndef V8_TARGET_OS_LINUX
#define V8_TARGET_OS_LINUX 1
#elif V8_TARGET_OS_LINUX != 1
#error "V8_TARGET_OS_LINUX must be 1"
#endif
#else
#error "Unsupported gov8 host"
#endif

#ifndef CPPGC_SUPPORTS_OBJECT_NAMES
#define CPPGC_SUPPORTS_OBJECT_NAMES 1
#else
#if CPPGC_SUPPORTS_OBJECT_NAMES != 1
#error "CPPGC_SUPPORTS_OBJECT_NAMES defined but not set to 1"
#endif
#endif  // CPPGC_SUPPORTS_OBJECT_NAMES

#ifndef CPPGC_CAGED_HEAP
#define CPPGC_CAGED_HEAP 1
#else
#if CPPGC_CAGED_HEAP != 1
#error "CPPGC_CAGED_HEAP defined but not set to 1"
#endif
#endif  // CPPGC_CAGED_HEAP

#ifndef CPPGC_YOUNG_GENERATION
#define CPPGC_YOUNG_GENERATION 1
#else
#if CPPGC_YOUNG_GENERATION != 1
#error "CPPGC_YOUNG_GENERATION defined but not set to 1"
#endif
#endif  // CPPGC_YOUNG_GENERATION

#ifndef CPPGC_POINTER_COMPRESSION
#define CPPGC_POINTER_COMPRESSION 1
#else
#if CPPGC_POINTER_COMPRESSION != 1
#error "CPPGC_POINTER_COMPRESSION defined but not set to 1"
#endif
#endif  // CPPGC_POINTER_COMPRESSION

#ifndef CPPGC_ENABLE_LARGER_CAGE
#define CPPGC_ENABLE_LARGER_CAGE 1
#else
#if CPPGC_ENABLE_LARGER_CAGE != 1
#error "CPPGC_ENABLE_LARGER_CAGE defined but not set to 1"
#endif
#endif  // CPPGC_ENABLE_LARGER_CAGE

#ifndef CPPGC_SLIM_WRITE_BARRIER
#define CPPGC_SLIM_WRITE_BARRIER 1
#else
#if CPPGC_SLIM_WRITE_BARRIER != 1
#error "CPPGC_SLIM_WRITE_BARRIER defined but not set to 1"
#endif
#endif  // CPPGC_SLIM_WRITE_BARRIER

#ifdef CPPGC_ENABLE_API_CHECKS
#error "CPPGC_ENABLE_API_CHECKS is defined but is disabled by V8's GN build arguments"
#endif  // CPPGC_ENABLE_API_CHECKS

#if defined(__linux__)
#ifndef CPPGC_ENABLE_OBJECT_SECTION_GCINFO
#define CPPGC_ENABLE_OBJECT_SECTION_GCINFO 1
#elif CPPGC_ENABLE_OBJECT_SECTION_GCINFO != 1
#error "CPPGC_ENABLE_OBJECT_SECTION_GCINFO must be 1"
#endif
#elif defined(CPPGC_ENABLE_OBJECT_SECTION_GCINFO)
#error "CPPGC_ENABLE_OBJECT_SECTION_GCINFO is defined but is disabled by V8's GN build arguments"
#endif  // CPPGC_ENABLE_OBJECT_SECTION_GCINFO

#ifdef CPPGC_ENABLE_SLOW_API_CHECKS
#error "CPPGC_ENABLE_SLOW_API_CHECKS is defined but is disabled by V8's GN build arguments"
#endif  // CPPGC_ENABLE_SLOW_API_CHECKS

#ifdef V8_COMPRESS_POINTERS_IN_SHARED_CAGE
#error "V8_COMPRESS_POINTERS_IN_SHARED_CAGE is defined but is disabled by V8's GN build arguments"
#endif  // V8_COMPRESS_POINTERS_IN_SHARED_CAGE

#ifdef V8_COMPRESS_ZONES
#error "V8_COMPRESS_ZONES is defined but is disabled by V8's GN build arguments"
#endif  // V8_COMPRESS_ZONES

#ifdef V8_ENABLE_CHECKS
#error "V8_ENABLE_CHECKS is defined but is disabled by V8's GN build arguments"
#endif  // V8_ENABLE_CHECKS

#ifdef V8_ENABLE_DIRECT_HANDLE
#error "V8_ENABLE_DIRECT_HANDLE is defined but is disabled by V8's GN build arguments"
#endif  // V8_ENABLE_DIRECT_HANDLE

#ifdef V8_ENABLE_MEMORY_ACCOUNTING_CHECKS
#error "V8_ENABLE_MEMORY_ACCOUNTING_CHECKS is defined but is disabled by V8's GN build arguments"
#endif  // V8_ENABLE_MEMORY_ACCOUNTING_CHECKS

#ifdef V8_ENABLE_SANDBOX
#error "V8_ENABLE_SANDBOX is defined but is disabled by V8's GN build arguments"
#endif  // V8_ENABLE_SANDBOX

#ifdef V8_IMMINENT_DEPRECATION_WARNINGS
#error "V8_IMMINENT_DEPRECATION_WARNINGS is defined but is disabled by V8's GN build arguments"
#endif  // V8_IMMINENT_DEPRECATION_WARNINGS

#ifdef V8_IS_TSAN
#error "V8_IS_TSAN is defined but is disabled by V8's GN build arguments"
#endif  // V8_IS_TSAN

#ifdef V8_MAP_PACKING
#error "V8_MAP_PACKING is defined but is disabled by V8's GN build arguments"
#endif  // V8_MAP_PACKING

#ifdef V8_MINORMS_STRING_SHORTCUTTING
#error "V8_MINORMS_STRING_SHORTCUTTING is defined but is disabled by V8's GN build arguments"
#endif  // V8_MINORMS_STRING_SHORTCUTTING

#ifdef V8_TARGET_ARCH_ARM64
#error "V8_TARGET_ARCH_ARM64 is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_ARCH_ARM64

#ifdef V8_TARGET_ARCH_LOONG64
#error "V8_TARGET_ARCH_LOONG64 is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_ARCH_LOONG64

#ifdef V8_TARGET_ARCH_MIPS64
#error "V8_TARGET_ARCH_MIPS64 is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_ARCH_MIPS64

#ifdef V8_TARGET_ARCH_PPC64
#error "V8_TARGET_ARCH_PPC64 is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_ARCH_PPC64

#ifdef V8_TARGET_OS_ANDROID
#error "V8_TARGET_OS_ANDROID is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_OS_ANDROID

#ifdef V8_TARGET_OS_CHROMEOS
#error "V8_TARGET_OS_CHROMEOS is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_OS_CHROMEOS

#ifdef V8_TARGET_OS_FUCHSIA
#error "V8_TARGET_OS_FUCHSIA is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_OS_FUCHSIA

#ifdef V8_TARGET_OS_IOS
#error "V8_TARGET_OS_IOS is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_OS_IOS

#if defined(_WIN32) && defined(V8_TARGET_OS_LINUX)
#error "Linux target macro on a Windows shim"
#endif
#if defined(__linux__) && defined(V8_TARGET_OS_WIN)
#error "Windows target macro on a Linux shim"
#endif

#ifdef V8_TARGET_OS_MACOS
#error "V8_TARGET_OS_MACOS is defined but is disabled by V8's GN build arguments"
#endif  // V8_TARGET_OS_MACOS

#ifdef V8_USE_PERFETTO
#error "V8_USE_PERFETTO is defined but is disabled by V8's GN build arguments"
#endif  // V8_USE_PERFETTO

#ifdef V8_USE_PERFETTO_JSON_EXPORT
#error "V8_USE_PERFETTO_JSON_EXPORT is defined but is disabled by V8's GN build arguments"
#endif  // V8_USE_PERFETTO_JSON_EXPORT

#ifdef V8_USE_PERFETTO_SDK
#error "V8_USE_PERFETTO_SDK is defined but is disabled by V8's GN build arguments"
#endif  // V8_USE_PERFETTO_SDK
