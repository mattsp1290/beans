#!/usr/bin/env python3
"""Run actual offline helper/Git/Beans mechanisms; does not emulate an agent."""
import os
from pathlib import Path
import sys
import unittest

sys.dont_write_bytecode = True
os.environ["PYTHONDONTWRITEBYTECODE"] = "1"
HERE = Path(__file__).resolve().parent
for source in sorted(HERE.glob("*.py")) + sorted((HERE.parent / "scripts").glob("*.py")):
    compile(source.read_text(), str(source), "exec")

if __name__ == "__main__":
    suite = unittest.defaultTestLoader.discover(str(HERE), pattern="test_*.py")
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    sys.exit(not result.wasSuccessful())
