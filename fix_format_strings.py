#!/usr/bin/env python3
"""
Script to fix non-constant format string issues in Go code.
This script identifies and fixes common patterns that cause go vet errors.
"""
import re
import sys
import os
import subprocess

def get_format_string_errors():
    """Get all format string errors from go vet"""
    try:
        result = subprocess.run(['go', 'vet', '-mod=vendor', './...'], 
                              capture_output=True, text=True, cwd='/home/mbasim/Documents/components/oc')
        errors = []
        for line in result.stderr.split('\n'):
            if 'non-constant format string' in line:
                # Parse: pkg/path/file.go:line:col: non-constant format string in call to func
                match = re.match(r'([^:]+):(\d+):\d+: non-constant format string in call to (.+)', line)
                if match:
                    errors.append({
                        'file': match.group(1),
                        'line': int(match.group(2)),
                        'function': match.group(3)
                    })
        return errors
    except Exception as e:
        print(f"Error getting format string errors: {e}")
        return []

def fix_format_string_in_file(filepath, line_num, function_name):
    """Fix a specific format string issue in a file"""
    full_path = f"/home/mbasim/Documents/components/oc/{filepath}"
    
    if not os.path.exists(full_path):
        print(f"File not found: {full_path}")
        return False
        
    try:
        with open(full_path, 'r') as f:
            lines = f.readlines()
            
        if line_num > len(lines):
            print(f"Line {line_num} out of range in {filepath}")
            return False
            
        original_line = lines[line_num - 1]
        fixed_line = original_line
        
        # Common patterns to fix
        patterns = [
            # fmt.Fprintf(out, variable) -> fmt.Fprintf(out, "%s", variable)
            (r'fmt\.Fprintf\(([^,]+),\s*([^,\)]+)\)', r'fmt.Fprintf(\1, "%s", \2)'),
            # fmt.Errorf(variable) -> fmt.Errorf("%s", variable)  
            (r'fmt\.Errorf\(([^,\)]+)\)', r'fmt.Errorf("%s", \1)'),
            # fmt.Sprintf(variable) -> variable (when used in another fmt function)
            (r'fmt\.Fprintf\(([^,]+),\s*fmt\.Sprintf\(([^)]+)\)\)', r'fmt.Fprintf(\1, \2)'),
            # util.UsageErrorf(variable) -> util.UsageErrorf("%s", variable)
            (r'util\.UsageErrorf\(([^,\)]+)\)', r'util.UsageErrorf("%s", \1)'),
        ]
        
        for pattern, replacement in patterns:
            if re.search(pattern, fixed_line):
                fixed_line = re.sub(pattern, replacement, fixed_line)
                break
                
        if fixed_line != original_line:
            lines[line_num - 1] = fixed_line
            with open(full_path, 'w') as f:
                f.writelines(lines)
            print(f"Fixed {filepath}:{line_num}")
            print(f"  Before: {original_line.strip()}")
            print(f"  After:  {fixed_line.strip()}")
            return True
        else:
            print(f"Could not automatically fix {filepath}:{line_num}")
            print(f"  Line: {original_line.strip()}")
            return False
            
    except Exception as e:
        print(f"Error fixing {filepath}:{line_num}: {e}")
        return False

def main():
    print("Finding format string errors...")
    errors = get_format_string_errors()
    
    if not errors:
        print("No format string errors found!")
        return
        
    print(f"Found {len(errors)} format string errors")
    
    fixed_count = 0
    for error in errors:
        if fix_format_string_in_file(error['file'], error['line'], error['function']):
            fixed_count += 1
            
    print(f"\nFixed {fixed_count} out of {len(errors)} errors")
    
    if fixed_count > 0:
        print("\nRunning go vet again to check remaining issues...")
        subprocess.run(['go', 'vet', '-mod=vendor', './...'], cwd='/home/mbasim/Documents/components/oc')

if __name__ == '__main__':
    main()