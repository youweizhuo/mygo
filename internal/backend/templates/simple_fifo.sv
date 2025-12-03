// Simple behavioral FIFO for local testing.
// This template is not referenced by the backend directly; point --fifo-src at
// this file (or copy it) when you need a known-good implementation.
// mygo:fifo_template
module mygo_fifo #(
  parameter integer WIDTH = 32,
  parameter integer DEPTH = 4,
  parameter integer ADDR_BITS = (DEPTH <= 1) ? 1 : $clog2(DEPTH),
  parameter integer COUNT_BITS = (DEPTH <= 1) ? 1 : $clog2(DEPTH + 1)
) (
  input  wire                   clk,
  input  wire                   rst,
  inout  wire [WIDTH-1:0]       in_data,
  inout  wire                   in_valid,
  inout  wire                   in_ready,
  inout  wire [WIDTH-1:0]       out_data,
  inout  wire                   out_valid,
  inout  wire                   out_ready
);
  reg [WIDTH-1:0] mem [0:DEPTH-1];
  reg [ADDR_BITS-1:0] wptr;
  reg [ADDR_BITS-1:0] rptr;
  reg [COUNT_BITS-1:0] count;

  wire ready_int = (count < COUNT_BITS'(DEPTH));
  wire valid_int = (count != 0);
  wire push = in_valid & ready_int;
  wire pop  = valid_int & out_ready;

  assign in_ready  = ready_int;
  assign out_valid = valid_int;
  assign out_data  = mem[rptr];

  always @(posedge clk) begin
    if (rst) begin
      wptr  <= {ADDR_BITS{1'b0}};
      rptr  <= {ADDR_BITS{1'b0}};
      count <= {COUNT_BITS{1'b0}};
    end else begin
      if (push) begin
        mem[wptr] <= in_data;
        if (DEPTH == 1) begin
          wptr <= {ADDR_BITS{1'b0}};
        end else if (wptr == ADDR_BITS'(DEPTH - 1)) begin
          wptr <= {ADDR_BITS{1'b0}};
        end else begin
          wptr <= wptr + 1'b1;
        end
      end
      if (pop) begin
        if (DEPTH == 1) begin
          rptr <= {ADDR_BITS{1'b0}};
        end else if (rptr == ADDR_BITS'(DEPTH - 1)) begin
          rptr <= {ADDR_BITS{1'b0}};
        end else begin
          rptr <= rptr + 1'b1;
        end
      end
      case ({push, pop})
        2'b10: count <= count + 1'b1;
        2'b01: count <= count - 1'b1;
        default: count <= count;
      endcase
    end
  end
endmodule
