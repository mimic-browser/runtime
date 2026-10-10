await new Promise((resolve) => setTimeout(resolve, 5));
throw new Error('async module failure sentinel');
