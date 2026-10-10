import { otherValue } from './cycle-other.js';
export function getValue() {
  return 7;
}
export const cycleValue = otherValue;
