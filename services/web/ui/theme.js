        (function() {
            const saved = localStorage.getItem('podsync-theme') || 
                (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'mocha' : 'latte');
            document.documentElement.setAttribute('data-theme', saved);
            document.documentElement.setAttribute('data-bs-theme', saved === 'mocha' ? 'dark' : 'light');
        })();
