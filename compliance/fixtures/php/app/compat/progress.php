<?php
// Progress output: three lines, each pushed past PHP, 300 ms apart.
require __DIR__ . '/../../_fixture.php';
fixture('app/compat/progress');
header('Content-Type: text/plain; charset=utf-8');
for ($i = 1; $i <= 3; $i++) {
    if ($i > 1) {
        usleep(300000);
    }
    echo "step $i\n";
    ob_flush();
    flush();
}
