import { useReducedMotion, type Transition } from "framer-motion";

export const easeOut = [0.23, 1, 0.32, 1] as const;

export const enterTransition: Transition = { duration: 0.42, ease: easeOut };

export function useEnter(y = 10) {
  const reduce = useReducedMotion();
  if (reduce) {
    return {
      initial: false as const,
      animate: { opacity: 1, y: 0 },
      exit: { opacity: 1, y: 0 },
      transition: { duration: 0 },
    };
  }
  return {
    initial: { opacity: 0, y },
    animate: { opacity: 1, y: 0 },
    exit: { opacity: 0, y: 6 },
    transition: enterTransition,
  };
}
