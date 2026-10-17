<?php
// $_POST as PHP parsed the body, and the warning parsing raised, if any.
require __DIR__ . '/_emit.php';
fixture('app/request/post');
emit(['post' => (object) $_POST, 'warning' => last_warning()]);
