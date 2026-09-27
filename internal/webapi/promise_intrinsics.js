// Platform algorithms create promises in their own realm. Author changes to
// the global constructor or its static methods do not select that machinery.
const platformPromise = Promise;
const platformPromiseResolve = Promise.resolve.bind(Promise);
const platformPromiseReject = Promise.reject.bind(Promise);
// Native combinators read their receiver's public resolve property. Use a
// private constructor which still returns an intrinsic Promise, so author
// replacement of Promise.resolve cannot enter an internal all/race operation.
function platformCombinatorPromise(executor) {
  return new platformPromise(executor);
}
Object.defineProperty(platformCombinatorPromise, 'resolve', { value: platformPromiseResolve });
const platformPromiseAll = Promise.all.bind(platformCombinatorPromise);
const platformPromiseRace = Promise.race.bind(platformCombinatorPromise);
