// Frozen Chrome 152 admits non-ASCII UTF-16 units, including lone surrogates.
// Element local names constrain the first unit; attributes and namespace
// prefixes do not. Remaining units exclude only the measured DOM delimiters.
const domNameRegexTest = RegExp.prototype.test;
const domNameRegexExec = RegExp.prototype.exec;
const domNameStringSplit = String.prototype.split;
const domNameInitial = /^[A-Za-z_:\u0080-\uffff]/;
const domTagDelimiters = /[\0\t\n\f\r />]/;
const domAttributeDelimiters = /[\0\t\n\f\r />=]/;
const domNameInvalidCharacter = (name, attribute) => {
  if (!attribute && !functionSourceApply(domNameRegexTest, domNameInitial, [name])) return name[0];
  return functionSourceApply(
    domNameRegexExec,
    attribute ? domAttributeDelimiters : domTagDelimiters,
    [name],
  )?.[0];
};
const validateDOMName = (name, attribute, operation, owner) => {
  if (name && domNameInvalidCharacter(name, attribute) === undefined) return name;
  const detail = attribute
    ? operation === 'createAttribute'
      ? "The localName provided ('" + name + "') contains an invalid character."
      : "'" + name + "' is not a valid attribute name."
    : "The tag name provided ('" + name + "') is not a valid name.";
  throw platformDOMException(
    "Failed to execute '" + operation + "' on '" + owner + "': " + detail,
    'InvalidCharacterError',
  );
};
const validateDOMQualifiedName = (namespace, name, attribute, operation, owner) => {
  const context = "Failed to execute '" + operation + "' on '" + owner + "': ";
  const fail = (detail) => {
    throw platformDOMException(context + detail, 'InvalidCharacterError');
  };
  if (!name) fail('The qualified name provided is empty.');
  const parts = functionSourceApply(domNameStringSplit, name, [':']);
  const prefix = parts.length > 1 ? parts[0] : null;
  const local = parts.length > 1 ? parts[1] : name;
  if (prefix === '')
    fail("The qualified name provided ('" + name + "') has an empty namespace prefix.");
  if (!local) fail("The qualified name provided ('" + name + "') has an empty local name.");
  const invalid =
    (prefix === null ? undefined : domNameInvalidCharacter(prefix, true)) ??
    domNameInvalidCharacter(local, attribute);
  if (invalid !== undefined)
    fail(
      "The qualified name provided ('" +
        name +
        "') contains the invalid character '" +
        invalid +
        "'.",
    );
  const canonical = prefix === null ? local : prefix + ':' + local;
  if (
    (prefix && !namespace) ||
    (prefix === 'xml' && namespace !== 'http://www.w3.org/XML/1998/namespace') ||
    ((canonical === 'xmlns' || prefix === 'xmlns') &&
      namespace !== 'http://www.w3.org/2000/xmlns/') ||
    (namespace === 'http://www.w3.org/2000/xmlns/' && canonical !== 'xmlns' && prefix !== 'xmlns')
  )
    throw platformDOMException('Invalid namespace', 'NamespaceError');
  return canonical;
};
